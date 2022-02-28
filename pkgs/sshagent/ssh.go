package sshagent

import (
	"bytes"
	"fmt"
	"kingraptor/pkgs/io/ioreader"
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
