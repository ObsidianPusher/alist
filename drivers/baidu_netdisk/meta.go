package baidu_netdisk

import (
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/op"
)

type Addition struct {
	RefreshToken string `json:"refresh_token"`
	driver.RootPath
	OrderBy                   string `json:"order_by" type:"select" options:"name,time,size" default:"name"`
	OrderDirection            string `json:"order_direction" type:"select" options:"asc,desc" default:"asc"`
	ClientID                  string `json:"client_id" required:"true" default:"iYCeC9g08h5vuP9UqvPHKKSVrKFXGa1v"`
	ClientSecret              string `json:"client_secret" required:"true" default:"jXiFMOPVPCWlO2M5CwWQzffpNPaGTRBG"`
	RedirectURI               string `json:"redirect_uri" default:"oob"`
	AuthCode                  string `json:"auth_code"`
	AccessToken               string
	UploadTaskConcurrency     int    `json:"upload_task_concurrency" type:"number" default:"1" help:"simultaneous upload tasks, 1<=n<=8"`
	DownloadTaskConcurrency   int    `json:"download_task_concurrency" type:"number" default:"3" help:"simultaneous background download tasks, 1<=n<=16"`
	DownloadPartSizeMB        int64  `json:"download_part_size_mb" type:"number" default:"8" help:"single task download part size in MB, 1<=n<=64"`
	TokenKeepaliveIntervalMin int    `json:"token_keepalive_interval_min" type:"number" default:"30" help:"keepalive interval in minutes, 5<=n<=1440"`
	UploadAPI                 string `json:"upload_api" default:"https://d.pcs.baidu.com"`
	CustomUploadPartSize      int64  `json:"custom_upload_part_size" type:"number" default:"0" help:"0 for auto"`
}

var config = driver.Config{
	Name:        "BaiduNetdisk",
	DefaultRoot: "/",
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &BaiduNetdisk{}
	})
}
