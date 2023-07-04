package client

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"github.com/RedTeamPentesting/kbtls"
	"github.com/armon/go-socks5"
	"github.com/hashicorp/yamux"
	"log"
	"net"
)

func StartClient(remoteAddr string, remoteSocksPort int, username string, password string, serverAuth string) error {
	tlsConfig, err := clientTLSConfig(serverAuth)
	if err != nil {
		return err
	}
	conn, err := tls.Dial("tcp", remoteAddr, tlsConfig)
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
	err = initConn(communicate, remoteSocksPort)
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

func initConn(conn net.Conn, port int) error {
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	_, err := conn.Write(portBytes)
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

func clientTLSConfig(connectionKey string) (*tls.Config, error) {
	key, err := kbtls.ParseConnectionKey(connectionKey)
	if err != nil {
		return nil, fmt.Errorf("parse connection key: %w", err)
	}
	cfg, err := kbtls.ClientTLSConfig(key)
	if err != nil {
		return nil, fmt.Errorf("configure TLS: %w", err)
	}
	return cfg, nil

}
