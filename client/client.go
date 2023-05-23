package client

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/hashicorp/yamux"
	"log"
	"net"
)

func StartClient(remoteAddr string, remoteSocksPort int, username string, password string, serverAuth string) error {
	conn, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	session, err := yamux.Server(conn, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	// 打开第一个连接进行通信
	communicate, err := session.Accept()
	if err != nil {
		return err
	}
	defer communicate.Close()
	err = initConn(communicate, remoteSocksPort, serverAuth)
	if err != nil {
		return err
	}
	clientLogger := log.New(&remoteLogger{conn: communicate}, "Client: ", log.Lshortfile)
	var proxyAuth socks5.Authenticator
	if username != "" && password != "" {
		proxyAuth = socks5.UserPassAuthenticator{Credentials: socks5.StaticCredentials{username: password}}
	} else {
		proxyAuth = socks5.NoAuthAuthenticator{}
	}
	// 这里先用普通的logger写过去，然后那边读出来再由logrus处理输出
	socks5Server, err := socks5.New(&socks5.Config{Logger: clientLogger, AuthMethods: []socks5.Authenticator{proxyAuth}})
	if err != nil {
		clientLogger.Println(err)
		return err
	}
	err = socks5Server.Serve(session)
	if err != nil {
		clientLogger.Println(err)
		return err
	}
	return nil
}

func sendToServer(data string, conn net.Conn) (int, error) {
	dataLenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(dataLenBytes, uint32(len(data)))
	return conn.Write(append(dataLenBytes, data...))
}

func initConn(conn net.Conn, port int, auth string) error {
	// do auth with server
	_, err := sendToServer(auth, conn)
	if err != nil {
		return err
	}
	authResult := make([]byte, 4)
	_, err = conn.Read(authResult)
	if err != nil {
		return errors.New("auth failed")
	}
	if binary.BigEndian.Uint32(authResult) == 0 {
		_ = conn.Close()
		return fmt.Errorf("auth server with secret: %s failed", auth)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	_, err = conn.Write(portBytes)
	if err != nil {
		return err
	}
	return nil
}

type remoteLogger struct {
	conn net.Conn
}

func (logger *remoteLogger) Write(data []byte) (int, error) {
	return sendToServer(string(data), logger.conn)
}
