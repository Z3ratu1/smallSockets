package main

import (
	"crypto/sha256"
	"encoding/base64"
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
		Short: "connect to server to provide socks5 service, server addr contain ip:port, listening port is the socks5 port server listening at",
		Long:  `eg: client 192.168.68.1:7890 12345 will connect back to server at 192.168.68.1:7890, and server will open port 12345 as socks5 proxy`,
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
			if len(auth) < 10 {
				return errors.New("your auth string need to be longer than 10 character")
			}
			authBytes := sha256.Sum256([]byte(auth))
			authBytesSlice := authBytes[:]
			auth = base64.RawStdEncoding.EncodeToString(authBytesSlice)
			return client.StartClient(args[0], port, proxyUser, proxyPass, auth)
		},
	}

	clientFlags := clientCmd.Flags()
	clientFlags.StringVarP(&proxyUser, "user", "u", "", "proxy username(optional)")
	clientFlags.StringVarP(&proxyPass, "pass", "p", "", "proxy password(optional)")
	clientCmd.MarkFlagsRequiredTogether("user", "pass")

	serverCmd := &cobra.Command{
		Use:   "server [control port]",
		Short: "start listener at control port, wait for client connect back",
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
			// 为什么要多定义一个变量才能用? tls的key要求是一个[32]byte，所以直接取hash刚好32位
			if len(auth) < 10 {
				return errors.New("your auth string need to be longer than 10 character")
			}
			authBytes := sha256.Sum256([]byte(auth))
			authBytesSlice := authBytes[:]
			auth = base64.RawStdEncoding.EncodeToString(authBytesSlice)
			return server.StartServer(port, auth, level)
		},
	}

	rootCmd.PersistentFlags().StringVarP(&auth, "auth", "a", "smallSockets", "auth string between client and server(optional)")
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
