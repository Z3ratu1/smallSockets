package server

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/RedTeamPentesting/kbtls"
	"github.com/hashicorp/yamux"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"strconv"
	"sync"
)

var serverLogger *logrus.Logger
var errorChan = make(chan error)

func StartServer(port int, auth string, logLevel logrus.Level) error {
	serverLogger = logrus.New()
	serverLogger.SetLevel(logLevel)
	defer close(errorChan)
	go handleErrorMsg()
	config, key, err := serverTLSConfig(auth)
	if err != nil {
		return err
	}
	logrus.Infof("server auth key: %s", key)
	clientListener, err := tls.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port), config)
	if err != nil {
		return err
	}
	serverLogger.Infof("listener start at 0.0.0.0:%d", port)
	defer clientListener.Close()
	for {
		conn, err := clientListener.Accept()
		if err != nil {
			// 这里所有的error都不影响程序继续运行，仅log
			// 如果listener挂了，就会出现closed err，程序退出
			if !errors.Is(err, net.ErrClosed) {
				serverLogger.Error(err)
				continue
			} else {
				serverLogger.Debugf("client listener at 0.0.0.0:%d closed", port)
				return err
			}
		}
		serverLogger.Infof("client from %s connected", conn.RemoteAddr())
		go handleClientConn(conn)
	}
}

func handleClientConn(conn net.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
		serverLogger.Debugf("client connection from %s closed", conn.RemoteAddr())
	}()
	// create client
	client, err := yamux.Client(conn, nil)
	if err != nil {
		errorChan <- err
		return
	}
	go func() {
		<-ctx.Done()
		_ = client.Close()
		serverLogger.Debugf("yamux client from %s closed", client.RemoteAddr())
	}()
	serverLogger.Debug("yamux Client opened")

	communicate, err := client.Open()
	if err != nil {
		errorChan <- err
		return
	}
	go func() {
		<-ctx.Done()
		_ = communicate.Close()
		serverLogger.Debugf("communication connection from %s closed", communicate.RemoteAddr())
	}()
	serverLogger.Debugf("communication connection from %s opened", communicate.RemoteAddr())
	// 用于控制socksListener的关闭
	// 先开一个连接进行通信，协商端口，错误回传之类的，如果这个通信连接挂了，也就意味着client连接挂了（大概）
	port, err := handleCommunicationConn(communicate, cancel)
	if err != nil {
		serverLogger.Debug("communication with client failed")
		errorChan <- err
		return
	}

	// 这里是socks5代理，得起普通的tcp监听
	socksListener, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		errorChan <- err
		return
	}
	go func() {
		<-ctx.Done()
		_ = socksListener.Close()
		serverLogger.Infof("proxy at 0.0.0.0:%d stopped", port)
	}()
	serverLogger.Infof("socks5 proxy for client from %s start at 0.0.0.0:%d", conn.RemoteAddr(), port)
	err = handleProxyConn(ctx, socksListener, client)
	if err != nil {
		if errors.Is(err, net.ErrClosed) {
			logrus.Debugf("proxy listener at %d closed", port)
		} else {
			errorChan <- err
		}
		return
	}
}

func handleCommunicationConn(conn net.Conn, cancelFunc context.CancelFunc) (int, error) {
	// auth check
	//serverLogger.Debugf("try to auth client with auth %s", auth)
	//clientAuth, err := readFromClient(conn)
	//if err != nil {
	//	return 0, errors.New("read client auth failed")
	//}
	//serverLogger.Debugf("get client auth %s", clientAuth)
	//authResult := make([]byte, 4)
	//if auth != clientAuth {
	//	binary.BigEndian.PutUint32(authResult, 0)
	//	_, _ = conn.Write(authResult)
	//	return 0, fmt.Errorf("invalid client auth: %s", clientAuth)
	//} else {
	//	serverLogger.Debug("auth client success")
	//	binary.BigEndian.PutUint32(authResult, 1)
	//	_, err = conn.Write(authResult)
	//	if err != nil {
	//		return 0, err
	//	}
	//}

	// 改用tls进行身份验证，这样子的话就不需要读auth了，直接读port
	portBytes := make([]byte, 2)
	_, err := conn.Read(portBytes)
	if err != nil {
		return 0, err
	}
	port := binary.BigEndian.Uint16(portBytes)
	serverLogger.Debugf("recieve socks5 proxy port %d", port)
	// 起一个协程持续读client发的消息并输出到errChan
	go func() {
		for {
			// 如果这个循环读出问题了，估计接下来的数据就都是乱的了，报错直接返回得了
			data, err := readFromClient(conn)
			if err != nil {
				// 这里是read读到了EOF，与Conn的Closed是同一种情况
				if !errors.Is(err, io.EOF) {
					errorChan <- err
				} else {
					serverLogger.Infof("client connection from %s closed", conn.RemoteAddr())
				}
				// 这个函数会把整个client的连接全都关掉
				cancelFunc()
				return
			}
			// 读出来的消息以info等级输出
			serverLogger.Infof("error from client %s: %s", conn.RemoteAddr(), data)
		}
	}()
	return int(port), nil
}

func handleProxyConn(ctx context.Context, socksListener net.Listener, session *yamux.Session) error {
	for {
		// 监听listener，直到listener挂掉
		src, err := socksListener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logrus.Debugf("proxy listener at %s closed", socksListener.Addr())
			}
			return err
		}
		go func() {
			<-ctx.Done()
			src.Close()
			serverLogger.Debugf("proxy src connection from %s closed", src.RemoteAddr())
		}()
		serverLogger.Debugf("accept proxy request from %s", src.RemoteAddr())
		dst, err := session.Open()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logrus.Debugf("client session from %s closed", session.RemoteAddr())
			}
			return err
		}
		go func() {
			<-ctx.Done()
			dst.Close()
			serverLogger.Debugf("proxy dst connection from %s closed", dst.RemoteAddr())
		}()
		serverLogger.Debugf("open proxy connection to %s", session.RemoteAddr())
		go join(src, dst)
	}
}

func join(src net.Conn, dst net.Conn) {
	defer src.Close()
	defer dst.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go pipe(src, dst, &wg)
	go pipe(dst, src, &wg)
	serverLogger.Debugf("start proxy traffic from %s to %s", src.RemoteAddr(), dst.RemoteAddr())
	wg.Wait()
}

func pipe(src net.Conn, dst net.Conn, group *sync.WaitGroup) {
	defer group.Done()
	buff := make([]byte, 4096)
	_, err := io.CopyBuffer(dst, src, buff)
	if err != nil {
		if !errors.Is(err, net.ErrClosed) {
			errorChan <- err
		} else {
			serverLogger.Debugf("connection from %s to %s closed", src.RemoteAddr(), dst.RemoteAddr())
		}
	}
}

func readFromClient(conn net.Conn) (string, error) {
	dataLenBytes := make([]byte, 4)
	_, err := conn.Read(dataLenBytes)
	if err != nil {
		return "", err
	}
	dataLen := binary.BigEndian.Uint32(dataLenBytes)
	if dataLen != 0 {
		data := make([]byte, dataLen)
		_, err = conn.Read(data)
		if err != nil {
			return "", err
		}
		return string(data), nil
	} else {
		return "", err
	}
}

func handleErrorMsg() {
	serverLogger.Debug("start listening errorChan")
	for {
		select {
		case err, ok := <-errorChan:
			if ok {
				serverLogger.Error(err)
			} else {
				close(errorChan)
				serverLogger.Debug("errorChan exit")
				return
			}
		}
	}
}

func serverTLSConfig(connectionKey string) (*tls.Config, kbtls.ConnectionKey, error) {
	var (
		key kbtls.ConnectionKey
		err error
	)

	if connectionKey != "" {
		key, err = kbtls.ParseConnectionKey(connectionKey)
		if err != nil {
			return nil, key, fmt.Errorf("parse connection key: %w", err)
		}
	} else {
		key, err = kbtls.GenerateConnectionKey()
		if err != nil {
			return nil, key, fmt.Errorf("generate connection key: %w", err)
		}
	}

	cfg, err := kbtls.ServerTLSConfig(key)
	if err != nil {
		return nil, key, fmt.Errorf("configure TLS: %w", err)
	}

	return cfg, key, nil
}
