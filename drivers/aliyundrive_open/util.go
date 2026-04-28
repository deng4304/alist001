package aliyundrive_open

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

// do others that not defined in Driver interface

const legacyOauthTokenURL = "https://api.alistgo.com/alist/ali_open/token"

type refreshRateLimitError struct {
	message    string
	retryAfter time.Duration
}

func (e *refreshRateLimitError) Error() string {
	if e.retryAfter > 0 {
		return fmt.Sprintf("%s, retry after %s", e.message, e.retryAfter.Round(time.Second))
	}
	return e.message
}

func (d *AliyundriveOpen) _refreshToken(ctx context.Context) (string, string, error) {
	if d.OauthTokenURL != "" && d.ClientID == "" {
		return d.refreshTokenWithOnlineAPI(ctx)
	}

	if d.ClientID == "" || d.ClientSecret == "" {
		return "", "", fmt.Errorf("empty ClientID or ClientSecret")
	}
	return d.refreshTokenWithPost(ctx, API_URL+"/oauth/access_token")
}

func (d *AliyundriveOpen) refreshTokenWithOnlineAPI(ctx context.Context) (string, string, error) {
	// New hosted renew endpoint uses a GET API and returns {"refresh_token","access_token","text"}.
	// Older hosted endpoints still expect the legacy POST payload, so we fall back when we detect that shape.
	var resp struct {
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token"`
		ErrorMessage string `json:"text"`
	}
	var e ErrResp
	if err := d.wait(ctx, limiterOther); err != nil {
		return "", "", err
	}
	res, err := base.RestyClient.R().
		SetResult(&resp).
		SetError(&e).
		SetQueryParams(map[string]string{
			"refresh_ui": d.RefreshToken,
			"server_use": "true",
			"driver_txt": "alicloud_qr",
		}).
		Get(d.OauthTokenURL)
	if err != nil {
		return "", "", err
	}
	if resp.RefreshToken != "" && resp.AccessToken != "" {
		return resp.RefreshToken, resp.AccessToken, nil
	}
	if resp.ErrorMessage != "" {
		if res != nil && res.StatusCode() == http.StatusTooManyRequests {
			return d.refreshTokenWithLegacyFallback(ctx, resp.ErrorMessage, retryAfterFromResponse(res))
		}
		return "", "", fmt.Errorf("failed to refresh token: %s", resp.ErrorMessage)
	}
	if res != nil && res.StatusCode() == http.StatusTooManyRequests {
		return d.refreshTokenWithLegacyFallback(ctx, http.StatusText(http.StatusTooManyRequests), retryAfterFromResponse(res))
	}
	if e.Code != "" || e.Message != "" {
		return d.refreshTokenWithPost(ctx, d.OauthTokenURL)
	}
	return "", "", fmt.Errorf("empty token returned from online API")
}

func (d *AliyundriveOpen) refreshTokenWithLegacyFallback(ctx context.Context, message string, retryAfter time.Duration) (string, string, error) {
	if d.OauthTokenURL != legacyOauthTokenURL {
		log.Warnf("[ali_open] online refresh API is rate-limited, trying legacy fallback: %s", legacyOauthTokenURL)
		if refresh, access, err := d.refreshTokenWithPost(ctx, legacyOauthTokenURL); err == nil {
			return refresh, access, nil
		} else if _, ok := err.(*refreshRateLimitError); !ok {
			return "", "", err
		}
	}
	return "", "", &refreshRateLimitError{
		message:    fmt.Sprintf("failed to refresh token: %s", message),
		retryAfter: retryAfter,
	}
}

func (d *AliyundriveOpen) refreshTokenWithPost(ctx context.Context, url string) (string, string, error) {
	//var resp base.TokenResp
	var e ErrResp
	if err := d.wait(ctx, limiterOther); err != nil {
		return "", "", err
	}
	res, err := base.RestyClient.R().
		//ForceContentType("application/json").
		SetBody(base.Json{
			"client_id":     d.ClientID,
			"client_secret": d.ClientSecret,
			"grant_type":    "refresh_token",
			"refresh_token": d.RefreshToken,
		}).
		//SetResult(&resp).
		SetError(&e).
		Post(url)
	if err != nil {
		return "", "", err
	}
	log.Debugf("[ali_open] refresh token response: %s", res.String())
	if res.StatusCode() == http.StatusTooManyRequests {
		message := e.Message
		if message == "" {
			message = http.StatusText(http.StatusTooManyRequests)
		}
		return "", "", &refreshRateLimitError{
			message:    fmt.Sprintf("failed to refresh token: %s", message),
			retryAfter: retryAfterFromResponse(res),
		}
	}
	if e.Code != "" {
		return "", "", fmt.Errorf("failed to refresh token: %s", e.Message)
	}
	refresh, access := utils.Json.Get(res.Body(), "refresh_token").ToString(), utils.Json.Get(res.Body(), "access_token").ToString()
	if refresh == "" {
		return "", "", fmt.Errorf("failed to refresh token: refresh token is empty, resp: %s", res.String())
	}
	curSub, err := getSub(d.RefreshToken)
	if err != nil {
		return "", "", err
	}
	newSub, err := getSub(refresh)
	if err != nil {
		return "", "", err
	}
	if curSub != newSub {
		return "", "", errors.New("failed to refresh token: sub not match")
	}
	return refresh, access, nil
}

func getSub(token string) (string, error) {
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return "", errors.New("not a jwt token because of invalid segments")
	}
	bs, err := base64.RawStdEncoding.DecodeString(segments[1])
	if err != nil {
		return "", errors.New("failed to decode jwt token")
	}
	return utils.Json.Get(bs, "sub").ToString(), nil
}

func (d *AliyundriveOpen) refreshToken(ctx context.Context) error {
	if d.ref != nil {
		return d.ref.refreshToken(ctx)
	}
	refresh, access, err := d._refreshToken(ctx)
	for i := 0; i < 3; i++ {
		if err == nil {
			break
		}
		if rateLimitErr, ok := err.(*refreshRateLimitError); ok {
			wait := rateLimitErr.retryAfter
			if wait <= 0 {
				wait = time.Duration(i+1) * 2 * time.Second
			}
			if wait > 15*time.Second {
				wait = 15 * time.Second
			}
			log.Warnf("[ali_open] %s", rateLimitErr.Error())
			time.Sleep(wait)
		} else {
			log.Errorf("[ali_open] failed to refresh token: %s", err)
		}
		refresh, access, err = d._refreshToken(ctx)
	}
	if err != nil {
		return err
	}
	log.Infof("[ali_open] token exchange: %s -> %s", d.RefreshToken, refresh)
	d.RefreshToken, d.AccessToken = refresh, access
	op.MustSaveDriverStorage(d)
	return nil
}

func retryAfterFromResponse(res *resty.Response) time.Duration {
	if res == nil {
		return 0
	}
	retryAfter := strings.TrimSpace(res.Header().Get("Retry-After"))
	if retryAfter == "" {
		return 0
	}
	if seconds, err := time.ParseDuration(retryAfter + "s"); err == nil {
		return seconds
	}
	if t, err := http.ParseTime(retryAfter); err == nil {
		return time.Until(t)
	}
	return 0
}

func (d *AliyundriveOpen) request(ctx context.Context, limitTy limiterType, uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error) {
	b, err, _ := d.requestReturnErrResp(ctx, limitTy, uri, method, callback, retry...)
	return b, err
}

func (d *AliyundriveOpen) requestReturnErrResp(ctx context.Context, limitTy limiterType, uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error, *ErrResp) {
	req := base.RestyClient.R()
	// TODO check whether access_token is expired
	req.SetHeader("Authorization", "Bearer "+d.getAccessToken())
	if method == http.MethodPost {
		req.SetHeader("Content-Type", "application/json")
	}
	if callback != nil {
		callback(req)
	}
	var e ErrResp
	req.SetError(&e)
	if err := d.wait(ctx, limitTy); err != nil {
		return nil, err, nil
	}
	res, err := req.Execute(method, API_URL+uri)
	if err != nil {
		if res != nil {
			log.Errorf("[aliyundrive_open] request error: %s", res.String())
		}
		return nil, err, nil
	}
	isRetry := len(retry) > 0 && retry[0]
	if e.Code != "" {
		if !isRetry && (utils.SliceContains([]string{"AccessTokenInvalid", "AccessTokenExpired", "I400JD"}, e.Code) || d.getAccessToken() == "") {
			err = d.refreshToken(ctx)
			if err != nil {
				return nil, err, nil
			}
			return d.requestReturnErrResp(ctx, limitTy, uri, method, callback, true)
		}
		return nil, fmt.Errorf("%s:%s", e.Code, e.Message), &e
	}
	return res.Body(), nil, nil
}

func (d *AliyundriveOpen) list(ctx context.Context, data base.Json) (*Files, error) {
	var resp Files
	_, err := d.request(ctx, limiterList, "/adrive/v1.0/openFile/list", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetResult(&resp)
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (d *AliyundriveOpen) getFiles(ctx context.Context, fileId string) ([]File, error) {
	marker := "first"
	res := make([]File, 0)
	for marker != "" {
		if marker == "first" {
			marker = ""
		}
		data := base.Json{
			"drive_id":        d.DriveId,
			"limit":           200,
			"marker":          marker,
			"order_by":        d.OrderBy,
			"order_direction": d.OrderDirection,
			"parent_file_id":  fileId,
			//"category":              "",
			//"type":                  "",
			//"video_thumbnail_time":  120000,
			//"video_thumbnail_width": 480,
			//"image_thumbnail_width": 480,
		}
		resp, err := d.list(ctx, data)
		if err != nil {
			return nil, err
		}
		marker = resp.NextMarker
		res = append(res, resp.Items...)
	}
	return res, nil
}

func getNowTime() (time.Time, string) {
	nowTime := time.Now()
	nowTimeStr := nowTime.Format("2006-01-02T15:04:05.000Z")
	return nowTime, nowTimeStr
}

func (d *AliyundriveOpen) getAccessToken() string {
	if d.ref != nil {
		return d.ref.getAccessToken()
	}
	return d.AccessToken
}

// Remove duplicate files with the same name in the given directory path,
// preserving the file with the given skipID if provided
func (d *AliyundriveOpen) removeDuplicateFiles(ctx context.Context, parentPath string, fileName string, skipID string) error {
	// Handle empty path (root directory) case
	if parentPath == "" {
		parentPath = "/"
	}

	// List all files in the parent directory
	files, err := op.List(ctx, d, parentPath, model.ListArgs{})
	if err != nil {
		return err
	}

	// Find all files with the same name
	var duplicates []model.Obj
	for _, file := range files {
		if file.GetName() == fileName && file.GetID() != skipID {
			duplicates = append(duplicates, file)
		}
	}

	// Remove all duplicates files, except the file with the given ID
	for _, file := range duplicates {
		err := d.Remove(ctx, file)
		if err != nil {
			return err
		}
	}

	return nil
}
