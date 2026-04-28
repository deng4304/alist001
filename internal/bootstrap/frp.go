package bootstrap

import (
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/frp"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/pkg/utils"
)

func InitFRP() {
	frp.Instance = frp.Init()
	if setting.GetBool(conf.FRPEnabled) {
		if err := frp.Instance.Start(); err != nil {
			utils.Log.Warnf("failed to start frp client: %v", err)
		} else {
			utils.Log.Info("frp client started")
		}
	}
}
