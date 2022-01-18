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
)

func GetUnixTime() int64 {
	return time.Now().Unix()
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
}

type ResourceUtil struct {
	Cpu         float64
	Ram         float64
	Disk        map[string]float64
	Name        string
	ID          int64
	IsCollected bool
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

func DoQuery(jobs <-chan ioreader.Node, results chan<- ResourceUtil) {

	cmds := []string{
		`sar 4 1 | grep Average | awk '{print ($3 + $5)}'`, //-- CPU Query
		`df -hP`,  //------------------------------------------- Disk Query
		`free -m`, //------------------------------------------- RAM Query
	}

	var err error

	for ne := range jobs {
		log.Printf("collecting info from ne %v", ne.Name)
		sshc := sshagent.SshAgent{}
		result := ResourceUtil{
			Name:        ne.Name,
			IsCollected: false,
		}
		sshc, err = sshagent.Init(ne)
		if err != nil {
			ProcessErrors(err, ne.Name)
			results <- result
			return
		}

		cmdRes := make(chan []string)
		errs := make(chan error)

		for _, c := range cmds {
			go sshc.Exec(c, cmdRes, errs)
		}

		for range cmds {
			select {
			case err := <-errs:
				ProcessErrors(err, ne.Name)
				results <- result
				return
			case res := <-cmdRes:
				result.ParseResult(res[0], res[1])
			case <-time.After(20 * time.Second):
				log.Printf("collection timeout for %v", ne.Name)
				results <- result
				return
			}
		}
		result.ID = GetUnixTime()

		result.IsCollected = true
		sshc.Disconnect()
		results <- result
	}
}

func (result *ResourceUtil) ParseResult(c, res string) {
	if strings.Contains(c, "awk") {
		result.Cpu = ParseCPU(res)
	} else if strings.Contains(c, "free -m") {
		result.Ram = ParseRAM(res)
	} else if strings.Contains(c, "df -hP") {
		result.Disk = ParseDisk(res)
	} else {
		log.Println(res)
	}
}

func (m *CriticalNeCounter) Wait30Min() {
	for m.RemainingTime > 0 {
		fmt.Println(m.RemainingTime, "-", m.Resource, "-", m.Name)
		time.Sleep(time.Second)
		m.Key.Lock()
		m.RemainingTime -= 1
		m.Key.Unlock()
	}
}

func ResetTimer(NodeResourceDb *map[string][]*CriticalNeCounter, ne ioreader.Node, timerType string) {
	for _, event := range (*NodeResourceDb)[ne.Name] {
		if event.Name == timerType {
			if event.RemainingTime > 0 {
				event.Key.Lock()
				event.RemainingTime = -1
				event.Key.Unlock()
			}
		}
	}
}

func InitMail(config ioreader.Config, name, resource, method string, value float64) {
	mailSubject := ""
	mailBody := ""
	if method == "single" {
		mailSubject = mail.CreateSubject(name, resource, value, method)
		mailBody = mail.CreateBody(name, resource, value, method)
	} else {
		mailSubject = mail.CreateSubject(name, resource, value, method)

		mailBody = mail.CreateBody(name, resource, value, method)
	}

	err := mail.MailConstructor(config.MailRelayIp, config.MailFrom, mailSubject, mailBody, config.MailTo)
	if err != nil {
		log.Printf("mail error %v ", err)
	} else {
		log.Println("mail notification dispatched!")
	}

}

func AssesResult(config ioreader.Config, NodeResourceDb *map[string][]*CriticalNeCounter, ne ioreader.Node, result ResourceUtil) []*CriticalResource { //Per NE
	AnyCriticalFound := false

	critical := []*CriticalResource{}
	_, NeAlreadyInDB := (*NodeResourceDb)[ne.Name]

	if result.Cpu >= ne.CpuThreshold {
		AnyCriticalFound = true
		critical = append(critical, &CriticalResource{
			Resource: "CPU",
			Value:    result.Cpu,
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
			})
		}
	}

	if AnyCriticalFound {

		if !NeAlreadyInDB {
			(*NodeResourceDb)[ne.Name] = []*CriticalNeCounter{}
		}

		for _, criticalresource := range critical {
			if strings.Contains(criticalresource.Resource, "Disk") {
				continue
			}
			eventExists := false
			for _, j := range (*NodeResourceDb)[ne.Name] {
				if j.Resource == criticalresource.Resource {
					eventExists = true
					if j.RemainingTime == -1 {
						log.Println("New High util. setting time to 200")
						j.Key.Lock()
						j.RemainingTime = config.RamCpuTimePeriod
						j.Key.Unlock()
						go j.Wait30Min()
						break
					} else if j.RemainingTime > 0 {
						break
					} else if j.RemainingTime == 0 {
						InitMail(config, j.Name, j.Resource, "single", j.Value)
						j.Key.Lock()
						j.RemainingTime = config.RamCpuTimePeriod
						j.Key.Unlock()
						go j.Wait30Min()
						break
					}
				}
			}
			if !eventExists {
				log.Println("New issue added to db")
				tmp := CriticalNeCounter{
					Name:          ne.Name,
					RemainingTime: config.RamCpuTimePeriod,
					Resource:      criticalresource.Resource,
					Value:         criticalresource.Value,
					Key:           &sync.Mutex{},
				}
				go tmp.Wait30Min()
				(*NodeResourceDb)[ne.Name] = append((*NodeResourceDb)[ne.Name], &tmp)
			}

		}

		return critical
	} else {
		return nil
	}
}
