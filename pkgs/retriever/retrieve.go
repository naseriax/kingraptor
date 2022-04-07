package retriever

import (
	"fmt"
	"kingraptor/pkgs/io/ioreader"
	"kingraptor/pkgs/mail"
	"kingraptor/pkgs/sshagent"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

var IsEnded = false
var IsSleeping = false

type DiskMailedObjects struct {
	Name     string
	Resource string
	Value    float64
	ID       int64
	Mailed   bool
}

type CriticalNeCounter struct {
	Name      string
	Resource  string
	Value     float64
	Key       *sync.Mutex
	HighCount int
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
		`sar 2 1 | grep Average | awk '{print ($3 + $5)}'`, //-- CPU Query
		`df -hP`,  //------------------------------------------- Disk Query
		`free -m`, //------------------------------------------- RAM Query
	}

	var err error
	result := ResourceUtil{
		Name:        ne.Name,
		IsCollected: false,
	}

	if config.SshTunnel {
		ne.IpAddress = "127.0.0.1"
		jumpserver := map[string]string{
			"ADDR":  fmt.Sprintf("%v:%v", config.SshGwIp, config.SshGwPort),
			"USER":  config.SshGwUser,
			"PASSW": config.SshGwPass,
		}
		portNo := make(chan string)
		errC := make(chan error)
		go sshagent.MakeTunnel(jumpserver, ne.IpAddress, ne.SshPort, portNo, errC)
		select {
		case p := <-portNo:
			ne.SshPort = p
		case err := <-errC:
			ProcessErrors(err, ne.Name)
			results <- result
			return
		}
	}

	sshc, err := sshagent.Init(&ne)
	if err != nil {
		ProcessErrors(err, "NE Session:"+ne.Name)
		results <- result
		return
	}
	defer sshc.Disconnect()

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
}

func (result *ResourceUtil) ParseResult(c string, res *string) {
	if strings.Contains(c, "sar") {
		result.Cpu = ParseCPU(*res)
	} else if strings.Contains(c, "free -m") {
		result.Ram = ParseRAM(*res)
	} else if strings.Contains(c, "df -hP") {
		result.Disk = ParseDisk(*res)
	} else {
		log.Println(res)
	}
}

func (m *DiskMailedObjects) StartMailTimer(mailInterval int) {
	m.Mailed = true
	for mailInterval > 0 {
		if IsEnded {
			return
		}
		time.Sleep(time.Second)
		mailInterval -= 1
	}
	m.Mailed = false
}

func ResetTimer(NodeResourceDb *map[string][]*CriticalNeCounter, ne *ioreader.Node, timerType string) {
	for ind := range (*NodeResourceDb)[ne.Name] {
		if (*NodeResourceDb)[ne.Name][ind].Resource == timerType {
			(*NodeResourceDb)[ne.Name][ind].Key.Lock()
			(*NodeResourceDb)[ne.Name][ind].HighCount = 0
			(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
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
		if config.Verbose {
			log.Printf("****************************** high CPU Utilization for NE %v: %v", ne.Name, result.Cpu)
		}
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
		if config.Verbose {
			log.Printf("****************************** high RAM Utilization for NE %v: %v", ne.Name, result.Ram)
		}
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
			if config.Verbose {
				log.Printf("****************************** high DISK Utilization for NE %v - %v : %v", ne.Name, mp, val)
			}
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
			if config.Verbose {
				log.Printf("Creating new object with HighCount=1/%v for %v - %v", config.HighCount, ne.Name, critical[ind].Resource)
			}
			newNodeinDb := CriticalNeCounter{
				Name:      ne.Name,
				Resource:  critical[ind].Resource,
				Value:     critical[ind].Value,
				Key:       &sync.Mutex{},
				HighCount: 1,
			}
			(*NodeResourceDb)[ne.Name] = append((*NodeResourceDb)[ne.Name], &newNodeinDb)
		} else {
			for ind := range (*NodeResourceDb)[ne.Name] {
				if (*NodeResourceDb)[ne.Name][ind].Resource == critical[ind].Resource {
					if config.Verbose {
						log.Printf("Increasing HighCount for %v - %v to %v / %v", ne.Name, critical[ind].Resource, (*NodeResourceDb)[ne.Name][ind].HighCount+1, config.HighCount)
					}
					(*NodeResourceDb)[ne.Name][ind].Key.Lock()
					(*NodeResourceDb)[ne.Name][ind].HighCount += 1
					(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
					if (*NodeResourceDb)[ne.Name][ind].HighCount >= config.HighCount {

						mailBody := []mail.MailBody{
							{
								Name:     (*NodeResourceDb)[ne.Name][ind].Name,
								Resource: (*NodeResourceDb)[ne.Name][ind].Resource,
								Value:    (*NodeResourceDb)[ne.Name][ind].Value,
							},
						}
						if config.EnableMail {
							if config.Verbose {
								log.Printf("Sending email for %v - %v HighCount: %v", ne.Name, critical[ind].Resource, (*NodeResourceDb)[ne.Name][ind].HighCount)
							}
							InitMail(config, mailBody)
						}
						if config.Verbose {
							log.Printf("Setting the Highcount=0/%v for %v - %v", config.HighCount, ne.Name, critical[ind].Resource)
						}
						(*NodeResourceDb)[ne.Name][ind].Key.Lock()
						(*NodeResourceDb)[ne.Name][ind].HighCount = 0
						(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
					}
					break
				}
			}
		}
	}
}
