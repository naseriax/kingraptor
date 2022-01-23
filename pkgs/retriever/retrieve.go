package retriever

import (
	"fmt"
	"kingraptor/pkgs/ioreader"
	"kingraptor/pkgs/mail"
	"kingraptor/pkgs/sshagent"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type DiskMailedObjects struct {
	Name     string
	Resource string
	Value    float64
	ID       int64
	Mailed   bool
}

type CriticalNeCounter struct {
	Name          string
	RemainingTime int
	Resource      string
	Value         float64
	Key           *sync.Mutex
}

type CriticalResource struct {
	Resource string
	Value    float64
	ID       int64
	Mailed   bool
}

type ResourceUtil struct {
	Cpu         float64
	Ram         float64
	Disk        map[string]float64
	Name        string
	ID          int64
	IsCollected bool
}

func GetUnixTime() int64 {
	return time.Now().Unix()
}

func ParseRAM(res string) float64 {
	memLines := strings.Split(res, "\n")

	var output float64
	for _, l := range memLines {
		if strings.Contains(l, "Mem:") {
			memInfo := strings.Fields(l)
			total, err := strconv.ParseFloat(memInfo[1], 64)
			if err != nil {
				log.Printf("%v", err)
			}
			avail, err := strconv.ParseFloat(memInfo[6], 64)
			if err != nil {
				log.Printf("%v", err)
			}
			output = 100 - ((avail * 100) / total)
			break
		}
	}
	val, err := strconv.ParseFloat(fmt.Sprintf("%.1f", output), 64)
	if err != nil {
		log.Printf("%v", err)
	}
	return val
}

func ParseCPU(res string) float64 {
	output, err := strconv.ParseFloat(strings.Replace(res, "\n", "", 1), 64)
	if err != nil {
		log.Printf("%v", err)
	}
	val, err := strconv.ParseFloat(fmt.Sprintf("%.1f", output), 64)
	if err != nil {
		log.Printf("%v", err)
	}
	return val
}

func ParseDisk(res string) map[string]float64 {
	output := map[string]float64{}
	dskLines := strings.Split(res, "\n")
	for _, l := range dskLines {
		if strings.Contains(l, "/") {
			dskInfo := strings.Fields(l)
			val, err := strconv.ParseFloat(strings.Replace(dskInfo[4], "%", "", 1), 64)
			if err != nil {
				log.Printf("%v", err)
			}
			output[dskInfo[5]] = val
		}
	}

	return output
}

func ProcessErrors(err error, neName string) {
	if strings.Contains(err.Error(), "attempted methods [none password]") {
		log.Printf("authentication error - wrong username/password for - %v", neName)
	} else if strings.Contains(err.Error(), "i/o timeout") {
		log.Printf("ne connection timeout - %v", neName)
	} else if strings.Contains(err.Error(), "network is unreachable") || strings.Contains(err.Error(), "no route to host") {
		log.Printf("ne is unreachable - %v", neName)
	} else if strings.Contains(err.Error(), "failed to create session") {
		log.Printf("failed to create new ssh session for %v", neName)
	} else if strings.Contains(err.Error(), "failed to run") {
		log.Printf("failed to run the command on - %v", neName)
	} else {
		log.Printf("connection error - %v - %v", neName, err)
	}
}

func DoQuery(config ioreader.Config, ne ioreader.Node, results chan<- ResourceUtil) {

	cmds := []string{
		`sar 4 1 | grep Average | awk '{print ($3 + $5)}'`, //-- CPU Query
		`df -hP`,  //------------------------------------------- Disk Query
		`free -m`, //------------------------------------------- RAM Query
	}

	TunnelDone := make(chan bool)
	var ClientConn *ssh.Client
	var err error
	TunnelConnection := false
	result := ResourceUtil{
		Name:        ne.Name,
		IsCollected: false,
	}

	//==================================================
	if config.SshTunnel {
		sshConfig := &ssh.ClientConfig{
			User: config.SshGwUser,
			Auth: []ssh.AuthMethod{
				ssh.Password(config.SshGwPass),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         time.Duration(5) * time.Second,
		}

		ClientConn, err = ssh.Dial("tcp", fmt.Sprintf("%v:%v", config.SshGwIp, config.SshGwPort), sshConfig)
		if err != nil {
			ProcessErrors(err, ne.Name)
			results <- result
			return
		}
		TunnelConnection = true
		tunReady := make(chan bool)
		go sshagent.Tunnel(tunReady, TunnelDone, ClientConn, fmt.Sprintf("localhost:%v", ne.Localport), fmt.Sprintf("%v:%v", ne.IpAddress, ne.SshPort))
		ne.IpAddress = "localhost"
		ne.SshPort = ne.Localport
		<-tunReady
	}

	//=========================================================
	sshc, err := sshagent.Init(&ne)
	if err != nil {
		ProcessErrors(err, ne.Name)
		if config.SshTunnel && TunnelConnection {
			select {
			case <-TunnelDone:
				ClientConn.Close()
			case <-time.After(15 * time.Second):
				log.Println("Tunnel Timeout.closing")
				ClientConn.Close()
			}
		}
		results <- result
		return
	}

	for _, c := range cmds {
		res, err := sshc.Exec(c)
		if err != nil {
			ProcessErrors(err, ne.Name)
			sshc.Disconnect()
			results <- result
			return
		}
		result.ParseResult(c, &res)
	}
	result.IsCollected = true
	result.ID = GetUnixTime()
	results <- result
	sshc.Disconnect()

	if config.SshTunnel && TunnelConnection {
		select {
		case <-TunnelDone:
			ClientConn.Close()
		case <-time.After(20 * time.Second):
			log.Println("Tunnel Timeout.closing")
			ClientConn.Close()
		}
	}
}

func (result *ResourceUtil) ParseResult(c string, res *string) {
	if strings.Contains(c, "awk") {
		result.Cpu = ParseCPU(*res)
	} else if strings.Contains(c, "free -m") {
		result.Ram = ParseRAM(*res)
	} else if strings.Contains(c, "df -hP") {
		result.Disk = ParseDisk(*res)
	} else {
		log.Println(res)
	}
}

func (m *CriticalNeCounter) StartCriticalTimer() {
	for m.RemainingTime > 0 {
		fmt.Printf("%v - %v - %v - %v\n", m.RemainingTime, m.Name, m.Resource, m.Value)
		m.Key.Lock()
		m.RemainingTime -= 1
		m.Key.Unlock()
		time.Sleep(time.Second)
	}
}

func (m *DiskMailedObjects) StartMailTimer(mailInterval int) {
	m.Mailed = true
	<-time.After(time.Duration(mailInterval) * time.Second)
	m.Mailed = false
}

func ResetTimer(NodeResourceDb *map[string][]*CriticalNeCounter, ne *ioreader.Node, timerType string) {
	for ind := range (*NodeResourceDb)[ne.Name] {
		if (*NodeResourceDb)[ne.Name][ind].Resource == timerType {
			if (*NodeResourceDb)[ne.Name][ind].RemainingTime > 0 {
				(*NodeResourceDb)[ne.Name][ind].Key.Lock()
				(*NodeResourceDb)[ne.Name][ind].RemainingTime = -1
				(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
			}
			break
		}
	}
}

func InitMail(config ioreader.Config, mailBodies []mail.MailBody) {
	mailSubject := ""
	mailBody := ""
	mailSubject = mail.CreateSubject()
	mailBody = mail.CreateBody(mailBodies)
	err := mail.MailConstructor(config.MailRelayIp, config.MailFrom, mailSubject, mailBody, config.MailTo)
	if err != nil {
		log.Printf("mail error %v ", err)
	} else {
		log.Println("mail notification dispatched!")
	}
}

func AssesResult(config ioreader.Config, NodeResourceDb *map[string][]*CriticalNeCounter, ne *ioreader.Node, result ResourceUtil) []*CriticalResource { //Per NE
	AnyCriticalFound := false

	critical := []*CriticalResource{}
	_, NeAlreadyInDB := (*NodeResourceDb)[ne.Name]

	if result.Cpu >= ne.CpuThreshold {
		AnyCriticalFound = true
		critical = append(critical, &CriticalResource{
			Resource: "CPU",
			Value:    result.Cpu,
			ID:       result.ID,
			Mailed:   false,
		})
	} else {
		if NeAlreadyInDB {
			ResetTimer(NodeResourceDb, ne, "CPU")
		}
	}
	if result.Ram >= ne.RamThreshold {
		AnyCriticalFound = true
		critical = append(critical, &CriticalResource{
			Resource: "RAM",
			Value:    result.Ram,
			ID:       result.ID,
			Mailed:   false,
		})

	} else {
		if NeAlreadyInDB {
			ResetTimer(NodeResourceDb, ne, "RAM")
		}
	}

	for mp, val := range result.Disk {
		if val >= ne.DiskThreshold {
			AnyCriticalFound = true
			critical = append(critical, &CriticalResource{
				Resource: fmt.Sprintf("Disk: %v", mp),
				Value:    val,
				ID:       result.ID,
				Mailed:   false,
			})
		}
	}

	if AnyCriticalFound {
		if !NeAlreadyInDB {
			(*NodeResourceDb)[ne.Name] = []*CriticalNeCounter{}
		}

		VerifyTimer(critical, NodeResourceDb, ne, config)
		return critical
	} else {
		return nil
	}
}

func VerifyTimer(critical []*CriticalResource, NodeResourceDb *map[string][]*CriticalNeCounter, ne *ioreader.Node, config ioreader.Config) {
	for ind := range critical {
		if strings.Contains(critical[ind].Resource, "Disk") {
			continue
		}
		if len((*NodeResourceDb)[ne.Name]) == 0 {
			newNodeinDb := CriticalNeCounter{
				Name:          ne.Name,
				RemainingTime: config.RamCpuTimePeriod,
				Resource:      critical[ind].Resource,
				Value:         critical[ind].Value,
				Key:           &sync.Mutex{},
			}
			go newNodeinDb.StartCriticalTimer()
			(*NodeResourceDb)[ne.Name] = append((*NodeResourceDb)[ne.Name], &newNodeinDb)
		} else {
			for ind := range (*NodeResourceDb)[ne.Name] {
				if (*NodeResourceDb)[ne.Name][ind].Resource == critical[ind].Resource {
					if (*NodeResourceDb)[ne.Name][ind].RemainingTime < 0 {
						(*NodeResourceDb)[ne.Name][ind].Key.Lock()
						(*NodeResourceDb)[ne.Name][ind].RemainingTime = config.RamCpuTimePeriod
						(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
						go (*NodeResourceDb)[ne.Name][ind].StartCriticalTimer()
					} else if (*NodeResourceDb)[ne.Name][ind].RemainingTime == 0 {
						mailBody := []mail.MailBody{
							{
								Name:     (*NodeResourceDb)[ne.Name][ind].Name,
								Resource: (*NodeResourceDb)[ne.Name][ind].Resource,
								Value:    (*NodeResourceDb)[ne.Name][ind].Value,
							},
						}
						if config.EnableMail {
							InitMail(config, mailBody)
						}
						(*NodeResourceDb)[ne.Name][ind].Key.Lock()
						(*NodeResourceDb)[ne.Name][ind].RemainingTime = config.RamCpuTimePeriod
						(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
						go (*NodeResourceDb)[ne.Name][ind].StartCriticalTimer()
					}
					break
				}
			}
		}
	}
}
