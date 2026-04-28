package frp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/cmd/flags"
	frpclient "github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	frplog "github.com/fatedier/frp/pkg/util/log"
	log "github.com/sirupsen/logrus"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/setting"
)

// Instance is the global FRP manager.
var Instance *Manager

// Manager controls the lifecycle of the embedded FRP client.
type Manager struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	status  string
	logs    []string
	logPath string
}

const maxLogEntries = 300
const maxTailBytes = 512 * 1024

// RuntimeInfo contains FRP runtime status and recent logs.
type RuntimeInfo struct {
	Status string   `json:"status"`
	Logs   []string `json:"logs"`
}

// Init creates and returns a new Manager.
func Init() *Manager {
	m := &Manager{
		status:  "stopped",
		logPath: filepath.Join(flags.DataDir, "log", "frp.log"),
	}
	m.logs = append(m.logs, fmt.Sprintf("[%s] initialized", time.Now().Format(time.RFC3339)))
	return m
}

// Start builds the FRP config from settings and starts the client.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.appendLogLocked("start skipped: already running")
		return nil // already running
	}

	cfg, proxyCfgs, err := buildConfig()
	if err != nil {
		m.status = "error: " + err.Error()
		m.appendLogLocked("start failed: %s", err.Error())
		return err
	}
	cfg.Log.To = m.logPath
	cfg.Log.Level = "info"
	cfg.Log.MaxDays = 7
	if err := os.MkdirAll(filepath.Dir(m.logPath), 0o755); err != nil {
		m.status = "error: " + err.Error()
		m.appendLogLocked("init log dir failed: %s", err.Error())
		return err
	}
	frplog.InitLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), true)

	configSource := source.NewConfigSource()
	if err := configSource.ReplaceAll(proxyCfgs, nil); err != nil {
		m.status = "error: " + err.Error()
		m.appendLogLocked("replace config failed: %s", err.Error())
		return err
	}
	aggregator := source.NewAggregator(configSource)

	svr, err := frpclient.NewService(frpclient.ServiceOptions{
		Common:                 cfg,
		ConfigSourceAggregator: aggregator,
	})
	if err != nil {
		m.status = "error: " + err.Error()
		m.appendLogLocked("create service failed: %s", err.Error())
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.status = "running"
	m.appendLogLocked("service started")
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()
		if err := svr.Run(ctx); err != nil && ctx.Err() == nil {
			// Context was not cancelled, so this is an unexpected error.
			log.Warnf("frp client stopped unexpectedly: %v", err)
			m.mu.Lock()
			m.status = "error: " + err.Error()
			m.appendLogLocked("service stopped unexpectedly: %s", err.Error())
			m.cancel = nil
			m.mu.Unlock()
		}
	}()

	return nil
}

// Stop gracefully shuts down the FRP client.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()

	if cancel != nil {
		m.appendLog("stopping service")
		cancel()
		m.wg.Wait()
	}

	m.mu.Lock()
	m.status = "stopped"
	m.appendLogLocked("service stopped")
	m.mu.Unlock()
}

// Restart stops any running client and starts a fresh one with current settings.
func (m *Manager) Restart() error {
	m.appendLog("restarting service")
	m.Stop()
	return m.Start()
}

// Status returns the current status string: "running", "stopped", or "error: <msg>".
func (m *Manager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Runtime returns status and latest logs.
func (m *Manager) Runtime(limit int) RuntimeInfo {
	m.mu.Lock()
	status := m.status
	logPath := m.logPath
	m.mu.Unlock()

	logs, err := readLogTail(logPath, limit)
	if err != nil {
		m.mu.Lock()
		logs = m.copyLogsLocked(limit)
		m.mu.Unlock()
	}

	return RuntimeInfo{
		Status: status,
		Logs:   logs,
	}
}

func (m *Manager) appendLog(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendLogLocked(format, args...)
}

func (m *Manager) appendLogLocked(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	entry := fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), line)
	m.logs = append(m.logs, entry)
	if len(m.logs) > maxLogEntries {
		m.logs = m.logs[len(m.logs)-maxLogEntries:]
	}
}

func (m *Manager) copyLogsLocked(limit int) []string {
	if limit <= 0 || limit > maxLogEntries {
		limit = maxLogEntries
	}
	total := len(m.logs)
	if total <= limit {
		return append([]string(nil), m.logs...)
	}
	return append([]string(nil), m.logs[total-limit:]...)
}

func readLogTail(path string, limit int) ([]string, error) {
	if limit <= 0 || limit > maxLogEntries {
		limit = maxLogEntries
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	start := int64(0)
	if size > maxTailBytes {
		start = size - maxTailBytes
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	if start > 0 {
		if idx := bytes.IndexByte(buf, '\n'); idx >= 0 && idx+1 < len(buf) {
			buf = buf[idx+1:]
		}
	}
	text := strings.TrimRight(string(buf), "\n")
	if text == "" {
		return []string{}, nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= limit {
		return lines, nil
	}
	return lines[len(lines)-limit:], nil
}

func buildConfig() (*v1.ClientCommonConfig, []v1.ProxyConfigurer, error) {
	serverAddr := setting.GetStr(conf.FRPServerAddr)
	if serverAddr == "" {
		return nil, nil, fmt.Errorf("frp server address is required")
	}

	serverPort := setting.GetInt(conf.FRPServerPort, 7000)
	authToken := setting.GetStr(conf.FRPAuthToken)
	proxyName := setting.GetStr(conf.FRPProxyName, "alist")
	proxyType := setting.GetStr(conf.FRPProxyType, "http")
	customDomain := setting.GetStr(conf.FRPCustomDomain)
	subdomain := setting.GetStr(conf.FRPSubdomain)
	remotePort := setting.GetInt(conf.FRPRemotePort, 0)
	localPort := setting.GetInt(conf.FRPLocalPort, 5244)
	tlsEnable := setting.GetBool(conf.FRPTLSEnable)
	stcpSecretKey := setting.GetStr(conf.FRPSTCPSecretKey)

	cfg := &v1.ClientCommonConfig{
		ServerAddr: serverAddr,
		ServerPort: serverPort,
		Auth: v1.AuthClientConfig{
			Method: v1.AuthMethodToken,
			Token:  authToken,
		},
	}
	if tlsEnable {
		enabled := true
		cfg.Transport.TLS.Enable = &enabled
	}

	backend := v1.ProxyBackend{
		LocalIP:   "127.0.0.1",
		LocalPort: localPort,
	}

	var proxyCfgs []v1.ProxyConfigurer

	switch proxyType {
	case "http":
		p := &v1.HTTPProxyConfig{}
		p.Name = proxyName
		p.Type = "http"
		p.ProxyBackend = backend
		if customDomain != "" {
			p.CustomDomains = []string{customDomain}
		}
		if subdomain != "" {
			p.SubDomain = subdomain
		}
		proxyCfgs = append(proxyCfgs, p)

	case "https":
		p := &v1.HTTPSProxyConfig{}
		p.Name = proxyName
		p.Type = "https"
		p.ProxyBackend = backend
		if customDomain != "" {
			p.CustomDomains = []string{customDomain}
		}
		if subdomain != "" {
			p.SubDomain = subdomain
		}
		proxyCfgs = append(proxyCfgs, p)

	case "tcp":
		if remotePort <= 0 {
			return nil, nil, fmt.Errorf("remote_port is required for tcp proxy type")
		}
		p := &v1.TCPProxyConfig{}
		p.Name = proxyName
		p.Type = "tcp"
		p.ProxyBackend = backend
		p.RemotePort = remotePort
		proxyCfgs = append(proxyCfgs, p)

	case "stcp":
		p := &v1.STCPProxyConfig{}
		p.Name = proxyName
		p.Type = "stcp"
		p.ProxyBackend = backend
		p.Secretkey = stcpSecretKey
		p.AllowUsers = []string{"*"}
		proxyCfgs = append(proxyCfgs, p)

	default:
		return nil, nil, fmt.Errorf("unsupported proxy type: %s", proxyType)
	}

	return cfg, proxyCfgs, nil
}
