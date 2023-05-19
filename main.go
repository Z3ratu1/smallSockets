package main

import (
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"net"
	"os"
	"smallSockets/client"
	"smallSockets/server"
	"strconv"
)

var rootCmd *cobra.Command

func init() {
	var proxyUser string
	var proxyPass string
	var auth string
	var logLevel string

	rootCmd = &cobra.Command{
		Use:   "smallSocks",
		Short: "A simple NAT traversal tool",
		Long:  "A simple NAT traversal tool that provide socks5 proxy",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	clientCmd := &cobra.Command{
		Use:   "client [server addr] [listening port]",
		Short: "connect to server to provide socks5 service",
		Long: `connect to server's control port to start socks5 service, <listen port> is the port that server listen.` +
			`you may provide username and password for socks5 service(optional)`,
		Args: func(cmd *cobra.Command, args []string) error {
			// Optionally run one of the validators provided by cobra
			// ExactArgs的返回值是一个函数，这个函数再对args进行检查然后返回error...
			// 这里输入的args是不包括文件名从0开始计数的
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return err
			}
			// Run the custom validation logic
			_, err := net.ResolveTCPAddr("tcp", args[0])
			if err != nil {
				return err
			}
			port, err := strconv.Atoi(args[1])
			if err != nil {
				return err
			}
			if port > 65535 || port < 1 {
				return errors.New("invalid port " + args[1])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := strconv.Atoi(args[1])
			return client.StartClient(args[0], port, proxyUser, proxyPass, auth)
		},
	}

	clientFlags := clientCmd.Flags()
	clientFlags.StringVarP(&proxyUser, "user", "u", "", "proxy username(optional)")
	clientFlags.StringVarP(&proxyPass, "pass", "p", "", "proxy password(optional)")
	clientCmd.MarkFlagsRequiredTogether("user", "pass")

	serverCmd := &cobra.Command{
		Use:   "server [control port]",
		Short: "start server at control port",
		Long:  "start server at control port, the socks5 proxy port is specified by client",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			port, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			if port > 65535 || port < 1 {
				return errors.New("invalid port " + args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := strconv.Atoi(args[0])
			var level logrus.Level
			switch logLevel {
			case "info":
				level = logrus.InfoLevel
			case "debug":
				level = logrus.DebugLevel
			case "error":
				level = logrus.ErrorLevel
			default:
				level = logrus.InfoLevel
			}
			return server.StartServer(port, auth, level)
		},
	}

	rootCmd.PersistentFlags().StringVarP(&auth, "auth", "a", "", "auth string between client and server(optional)")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "level", "l", "", "log lever(debug/info/error)(optional)")
	rootCmd.AddCommand(clientCmd)
	rootCmd.AddCommand(serverCmd)
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(0)
	}
}
