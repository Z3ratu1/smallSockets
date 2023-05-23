package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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
	clientListener, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	serverLogger.Infof("client listener start at 0.0.0.0:%d", port)
	defer clientListener.Close()
	for {
		conn, err := clientListener.Accept()
		if err != nil {
			// 这里所有的error都不影响程序继续运行，仅log
			// 如果error是Closed就直接跳过了
			if !errors.Is(err, net.ErrClosed) {
				serverLogger.Error(err)
				continue
			} else {
				serverLogger.Debugf("client listener at 0.0.0.0:%d closed", port)
				return err
			}
		}
		serverLogger.Infof("client from %s connected", conn.RemoteAddr())
		go handleClientConn(conn, auth)
	}
}

func handleClientConn(conn net.Conn, auth string) {
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
	port, err := handleCommunicationConn(communicate, auth, cancel)
	if err != nil {
		serverLogger.Debug("communication with client failed")
		errorChan <- err
		return
	}

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
	serverLogger.Infof("socks5 proxy start at 0.0.0.0:%d", port)
	for {
		// 连上来的client挂掉后，socksListener应当被关闭，然后对应该error return掉，将之前的资源全部释放掉
		err = handleProxyConn(ctx, socksListener, client)
		if err != nil {
			// 这里不能随便return，一旦return之前的defer close就全关了，只有当listener挂掉的时候才返回
			if !errors.Is(err, net.ErrClosed) {
				errorChan <- err
			} else {
				return
			}
		}
	}
}

func handleCommunicationConn(conn net.Conn, auth string, cancelFunc context.CancelFunc) (int, error) {
	// auth check
	serverLogger.Debugf("try to auth client with auth %s", auth)
	clientAuth, err := readFromClient(conn)
	if err != nil {
		return 0, errors.New("read client auth failed")
	}
	serverLogger.Debugf("get client auth %s", clientAuth)
	authResult := make([]byte, 4)
	if auth != clientAuth {
		binary.BigEndian.PutUint32(authResult, 0)
		_, _ = conn.Write(authResult)
		return 0, fmt.Errorf("invalid client auth: %s", clientAuth)
	} else {
		serverLogger.Debug("auth client success")
		binary.BigEndian.PutUint32(authResult, 1)
		_, err = conn.Write(authResult)
		if err != nil {
			return 0, err
		}
	}

	portBytes := make([]byte, 2)
	_, err = conn.Read(portBytes)
	if err != nil {
		return 0, nil
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
			serverLogger.Info(data)
		}
	}()
	return int(port), nil
}

func handleProxyConn(ctx context.Context, socksListener net.Listener, session *yamux.Session) error {
	src, err := socksListener.Accept()
	if err != nil {
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
		return err
	}
	go func() {
		<-ctx.Done()
		dst.Close()
		serverLogger.Debugf("proxy dst connection from %s closed", dst.RemoteAddr())
	}()
	serverLogger.Debugf("open proxy connection to %s", session.RemoteAddr())
	go join(src, dst)
	return nil
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
