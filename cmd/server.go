package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alist-org/alist/v3/cmd/flags"
	"github.com/alist-org/alist/v3/internal/bootstrap"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/s3server"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const gracefulShutdownTimeout = 15 * time.Second

func isIgnorableServerErr(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, http.ErrServerClosed) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

// ServerCmd represents the server command
var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the server at the specified address",
	Long: `Start the server at the specified address
the address is defined in config file`,
	Run: func(cmd *cobra.Command, args []string) {
		Init()
		if conf.Conf.DelayedStart != 0 {
			utils.Log.Infof("delayed start for %d seconds", conf.Conf.DelayedStart)
			time.Sleep(time.Duration(conf.Conf.DelayedStart) * time.Second)
		}
		bootstrap.LoadStorages()
		bootstrap.InitTaskManager()
		s3server.Start()
		if !flags.Debug && !flags.Dev {
			gin.SetMode(gin.ReleaseMode)
		}
		r := gin.New()
		r.Use(gin.LoggerWithWriter(log.StandardLogger().Out), gin.RecoveryWithWriter(log.StandardLogger().Out))
		server.Init(r)
		var httpSrv, httpsSrv, unixSrv *http.Server
		serveErrCh := make(chan error, 3)
		reportServeErr := func(name string, err error) {
			if isIgnorableServerErr(err) {
				return
			}
			wrapped := fmt.Errorf("%s server runtime error: %w", name, err)
			select {
			case serveErrCh <- wrapped:
			default:
				utils.Log.Error(wrapped)
			}
		}
		if conf.Conf.Scheme.HttpPort != -1 {
			httpBase := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.Scheme.HttpPort)
			utils.Log.Infof("start HTTP server @ %s", httpBase)
			httpSrv = &http.Server{Addr: httpBase, Handler: r}
			utils.GoSafe("http-server-listen", func() {
				reportServeErr("HTTP", httpSrv.ListenAndServe())
			})
		}
		if conf.Conf.Scheme.HttpsPort != -1 {
			httpsBase := fmt.Sprintf("%s:%d", conf.Conf.Scheme.Address, conf.Conf.Scheme.HttpsPort)
			utils.Log.Infof("start HTTPS server @ %s", httpsBase)
			httpsSrv = &http.Server{Addr: httpsBase, Handler: r}
			utils.GoSafe("https-server-listen", func() {
				reportServeErr("HTTPS", httpsSrv.ListenAndServeTLS(conf.Conf.Scheme.CertFile, conf.Conf.Scheme.KeyFile))
			})
		}
		if conf.Conf.Scheme.UnixFile != "" {
			utils.Log.Infof("start unix server @ %s", conf.Conf.Scheme.UnixFile)
			unixSrv = &http.Server{Handler: r}
			utils.GoSafe("unix-server-listen", func() {
				listener, err := net.Listen("unix", conf.Conf.Scheme.UnixFile)
				if err != nil {
					reportServeErr("Unix", fmt.Errorf("failed to listen unix: %w", err))
					return
				}
				defer func() {
					_ = listener.Close()
				}()
				// set socket file permission
				mode, err := strconv.ParseUint(conf.Conf.Scheme.UnixFilePerm, 8, 32)
				if err != nil {
					utils.Log.Errorf("failed to parse socket file permission: %+v", err)
				} else {
					err = os.Chmod(conf.Conf.Scheme.UnixFile, os.FileMode(mode))
					if err != nil {
						utils.Log.Errorf("failed to chmod socket file: %+v", err)
					}
				}
				reportServeErr("Unix", unixSrv.Serve(listener))
			})
		}
		// Wait for interrupt signal to gracefully shutdown servers.
		quit := make(chan os.Signal, 1)
		// kill (no param) default send syscanll.SIGTERM
		// kill -2 is syscall.SIGINT
		// kill -9 is syscall. SIGKILL but can"t be catch, so don't need add it
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		var shutdownReason string
		select {
		case sig := <-quit:
			shutdownReason = fmt.Sprintf("signal=%s", sig.String())
		case err := <-serveErrCh:
			shutdownReason = fmt.Sprintf("runtime_error=%v", err)
			utils.Log.Error(err)
		}
		signal.Stop(quit)
		utils.Log.Printf("Shutdown server... (%s)", shutdownReason)
		var wg sync.WaitGroup
		shutdownServer := func(name string, srv *http.Server) {
			defer wg.Done()
			if srv == nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					utils.Log.Warnf("%s server graceful shutdown timed out after %s, force closing", name, gracefulShutdownTimeout)
					if closeErr := srv.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
						utils.Log.Errorf("%s server force close err: %v", name, closeErr)
					}
					return
				}
				if !errors.Is(err, http.ErrServerClosed) {
					utils.Log.Errorf("%s server shutdown err: %v", name, err)
				}
			}
		}
		if conf.Conf.Scheme.HttpPort != -1 {
			wg.Add(1)
			go shutdownServer("HTTP", httpSrv)
		}
		if conf.Conf.Scheme.HttpsPort != -1 {
			wg.Add(1)
			go shutdownServer("HTTPS", httpsSrv)
		}
		if conf.Conf.Scheme.UnixFile != "" {
			wg.Add(1)
			go shutdownServer("Unix", unixSrv)
		}
		wg.Wait()
		s3server.Stop()
		Release()
		utils.Log.Println("Server exit")
	},
}

func init() {
	RootCmd.AddCommand(ServerCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serverCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serverCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// OutAlistInit 暴露用于外部启动server的函数
func OutAlistInit() {
	var (
		cmd  *cobra.Command
		args []string
	)
	ServerCmd.Run(cmd, args)
}
