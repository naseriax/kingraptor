package sshagent

import (
	"bytes"
	"fmt"
	"io"
	"kingraptor/pkgs/io/ioreader"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

//SshAgent object contains all ssh connectivity info and tools.
type SshAgent struct {
	Host     string
	Name     string
	Port     string
	UserName string
	Password string
	Timeout  int
	Client   *ssh.Client
	Session  *ssh.Session
}

//Connect connects to the specified server and opens a session (Filling the Client and Session fields in SshAgent struct)
func (s *SshAgent) Connect() error {
	var err error

	config := &ssh.ClientConfig{
		User: s.UserName,
		Auth: []ssh.AuthMethod{
			ssh.Password(s.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(s.Timeout) * time.Second,
	}
	s.Client, err = ssh.Dial("tcp", fmt.Sprintf("%v:%v", s.Host, s.Port), config)
	if err != nil {
		return err
	}

	return nil
}

func (s *SshAgent) Exec(cmd string) (string, error) {
	var err error
	s.Session, err = s.Client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %v >> %v", cmd, err.Error())
	}
	defer s.Session.Close()
	var b bytes.Buffer
	s.Session.Stdout = &b
	if err := s.Session.Run(cmd); err != nil {
		return "", fmt.Errorf("failed to run: %v >> %v", cmd, err.Error())
	} else {
		return b.String(), nil
	}
}

//Disconnect closes the ssh sessoin and connection.
func (s *SshAgent) Disconnect() {
	s.Client.Close()
}

//Init initialises the ssh connection and returns the usable ssh agent.
func Init(ne *ioreader.Node) (*SshAgent, error) {
	sshagent := SshAgent{
		Host:     (*ne).IpAddress,
		Port:     (*ne).SshPort,
		Name:     (*ne).Name,
		UserName: (*ne).Username,
		Password: (*ne).Password,
		Timeout:  20,
	}

	err := sshagent.Connect()

	if err != nil {
		return &sshagent, err
	} else {
		return &sshagent, nil
	}
}

func errd(err error, openConns ...interface{}) error {
	if err != nil {
		for _, o := range openConns {
			if obj, ok := o.(net.Listener); ok {
				obj.Close()
			} else if obj, ok := o.(net.Conn); ok {
				obj.Close()
			} else if obj, ok := o.(*ssh.Client); ok {
				obj.Close()
			}
		}
		errContent := err.Error()
		if strings.Contains(errContent, "administratively prohibited (open failed)") {
			return fmt.Errorf("TCP forwarding failure! - Possibly the TCPForwarding is not allowed in the jump server! - Error details: %v", errContent)
		} else if strings.Contains(errContent, "i/o timeout") {
			return fmt.Errorf("jump server ip address is not reachable - Error details: %v", errContent)
		} else if strings.Contains(errContent, "No connection could be made because the target machine actively refused it") {
			return fmt.Errorf("jump server port number is not accessible - Error details: %v", errContent)
		} else if strings.Contains(errContent, "unable to authenticate") {
			return fmt.Errorf("jump server username or password is wrong - Error details: %v", errContent)
		} else if strings.Contains(errContent, "A socket operation was attempted to an unreachable host") {
			return fmt.Errorf("jump server is not routable - Error details: %v", errContent)
		} else if strings.Contains(errContent, "rejected: connect failed (Connection refused)") {
			return fmt.Errorf("connection from jump server to remote node is rejected. could it be wrong remote port number?! - Error details: %v", errContent)

		} else {
			return fmt.Errorf(errContent)
		}
	} else {
		return nil
	}
}

// func Pipe(copyProgress chan int, errch chan error, writer, reader net.Conn) {
// 	defer writer.Close()
// 	defer reader.Close()

// 	_, err := io.Copy(writer, reader)
// 	if err != nil {
// 		if strings.Contains(err.Error(), "An existing connection was forcibly closed by the remote host") {
// 			errch <- nil
// 		}
// 		log.Printf("failed to copy: %s", err)
// 		errch <- err
// 		return
// 	}
// 	copyProgress <- 1
// }

func Pipe(errch chan error, writer, reader net.Conn) {
	defer writer.Close()
	defer reader.Close()
	_, err := io.Copy(writer, reader)

	if err != nil {
		if strings.Contains(err.Error(), "An existing connection was forcibly closed by the remote host") {
			errch <- nil
		}
	}
	errch <- err
}

//Tunnel(tunReady, ClientConn, fmt.Sprintf("%v:%v", ne.IpAddress, ne.SshPort))
func Tunnel(jumpserver map[string]string, remoteAddr string, localPortNo chan<- string, tunnelDone chan<- error) {
	var remoteCon net.Conn
	defer func(tunnelDone chan<- error) { tunnelDone <- nil }(tunnelDone)
	sshConfig := &ssh.ClientConfig{
		User:            jumpserver["USER"],
		Auth:            []ssh.AuthMethod{ssh.Password(jumpserver["PASSW"])},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(10) * time.Second,
	}
	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tunnelDone <- errd(err, nil)
		return
	}
	defer lst.Close()

	//localPortNo contains the opened port on localhost to accept the connect.
	localPortNo <- strings.Split(lst.Addr().String(), ":")[1]

	//Connect from Local to jump server
	l1, err := ssh.Dial("tcp", jumpserver["ADDR"], sshConfig)
	if err != nil {
		tunnelDone <- errd(err, lst)
		return
	}
	defer l1.Close()

	//Connect from Jumpserver to remote
	remoteDialChan := make(chan net.Conn)
	go func(remoteDialChan chan<- net.Conn) {
		remoteCon, err := l1.Dial("tcp", remoteAddr)
		if err != nil {
			tunnelDone <- errd(err, l1, lst)
			return
		}
		remoteDialChan <- remoteCon
	}(remoteDialChan)
	select {
	case remoteCon = <-remoteDialChan:
	case <-time.After(time.Duration(20) * time.Second):
		tunnelDone <- errd(fmt.Errorf("connection timeout (from Jumpserver to remote node)"), lst, l1)
		return
	}
	defer remoteCon.Close()

	//Wait for connection to localport
	localCon, err := lst.Accept()
	if err != nil {
		tunnelDone <- errd(err, remoteCon, l1, lst)
		return
	}
	defer localCon.Close()

	//Copy data from localport to tunnel
	errch := make(chan error)
	go Pipe(errch, remoteCon, localCon)
	go Pipe(errch, localCon, remoteCon)
	for i := 0; i < 2; i++ {
		if err := <-errch; err != nil {
			tunnelDone <- errd(err, localCon, remoteCon, l1, lst)
			return
		}
	}
}

// func Tunnel(tunReady chan<- string, TunnelDone chan<- bool, conn *ssh.Client, local, remote string) {
// 	lst, err := net.Listen("tcp", local)
// 	if err != nil {
// 		log.Println(err.Error())
// 		tunReady <- ""
// 		return
// 	}

// 	tunReady <- lst.Addr().String()
// 	here, err := lst.Accept()
// 	if err != nil {
// 		log.Printf("failed to accept the ssh connection - %v", err)
// 		lst.Close()
// 		return
// 	}
// 	thereOk := make(chan bool)
// 	thereErr := make(chan bool)
// 	go func(thereOk, thereErr chan bool, here net.Conn) {
// 		copyProgress := make(chan int)
// 		errch := make(chan error)
// 		there, err := conn.Dial("tcp", remote)
// 		if err != nil {
// 			log.Printf("failed to dial through the tunnel - TCP forwarding allowed?- %v", err)
// 			thereErr <- true
// 			return
// 		}
// 		thereOk <- true
// 		go Pipe(copyProgress, errch, there, here)
// 		go Pipe(copyProgress, errch, here, there)
// 		for i := 0; i < 2; i++ {
// 			select {
// 			case <-errch:
// 			case <-copyProgress:
// 			}
// 		}
// 		there.Close()
// 		here.Close()
// 		lst.Close()
// 		TunnelDone <- true
// 	}(thereOk, thereErr, here)

// 	select {
// 	case <-thereErr:
// 		here.Close()
// 		lst.Close()
// 		TunnelDone <- true
// 		return
// 	case <-thereOk:
// 	}
// }

func MakeTunnel(jumpserver map[string]string, ip, port string, portNo chan string, errCh chan error) {
	localPortNo := make(chan string)
	errC := make(chan error)
	go Tunnel(jumpserver, fmt.Sprintf("%v:%v", ip, port), localPortNo, errC)

	select {
	case localPort := <-localPortNo:
		portNo <- localPort
	case err := <-errC:
		errCh <- err
		return
	}

	if err := <-errC; err != nil {
		errCh <- err
	}
}
