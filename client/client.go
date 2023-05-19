package client

import (
	"encoding/binary"
	"github.com/armon/go-socks5"
	"github.com/hashicorp/yamux"
	"log"
	"net"
)

func StartClient(remoteAddr string, remoteSocksPort int, username string, password string, serverAuth string) error {
	//seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	//clientID := strconv.FormatUint(seed.Uint64(), 16)
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
	clientLogger := log.New(&remoteLogger{conn: communicate}, "", log.Lshortfile|log.Ltime)
	var proxyAuth socks5.Authenticator
	if username != "" && password != "" {
		proxyAuth = socks5.UserPassAuthenticator{Credentials: socks5.StaticCredentials{username: password}}
	} else {
		proxyAuth = socks5.NoAuthAuthenticator{}
	}
	// 这里先用普通的logger写过去，然后那边读出来再由logrus处理输出
	// TODO clientLogger往server写内容好像会出错
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
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	_, err := conn.Write(portBytes)
	if err != nil {
		return err
	}
	if auth != "" {
		_, err = sendToServer(auth, conn)
		if err != nil {
			return err
		}
	}
	return nil
}

type remoteLogger struct {
	conn net.Conn
}

func (logger *remoteLogger) Write(data []byte) (int, error) {
	return sendToServer(string(data), logger.conn)
}
