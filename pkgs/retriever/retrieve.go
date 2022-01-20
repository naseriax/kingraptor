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
	ID       int64
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

func DoQuery(nelogin, nelogout chan<- int, jobs <-chan ioreader.Node, results chan<- ResourceUtil) {

	cmds := []string{
		`sar 4 1 | grep Average | awk '{print ($3 + $5)}'`, //-- CPU Query
		`df -hP`,  //------------------------------------------- Disk Query
		`free -m`, //------------------------------------------- RAM Query
	}

	for ne := range jobs {
		result := ResourceUtil{
			Name:        ne.Name,
			IsCollected: false,
		}
		sshc, err := sshagent.Init(&ne)
		if err != nil {
			ProcessErrors(err, ne.Name)
			nelogout <- 1
			results <- result
			return
		}
		nelogin <- 1

		for _, c := range cmds {
			res, err := sshc.Exec(c)
			if err != nil {
				ProcessErrors(err, ne.Name)
				sshc.Disconnect()
				nelogout <- 1
				results <- result
				return
			}
			result.ParseResult(c, &res)
		}
		result.IsCollected = true
		result.ID = GetUnixTime()
		results <- result

		sshc.Disconnect()
		nelogout <- 1
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

func (m *CriticalNeCounter) Wait30Min() {
	fmt.Printf("%+v\n", m)
	for m.RemainingTime > 0 {
		fmt.Printf("%v - %v - %v - %v\n", m.RemainingTime, m.Name, m.Resource, m.Value)
		m.Key.Lock()
		m.RemainingTime -= 1
		m.Key.Unlock()
		time.Sleep(time.Second)
	}
}

func ResetTimer(NodeResourceDb *map[string][]*CriticalNeCounter, ne *ioreader.Node, timerType string) {
	log.Printf("Calling reset Timer for %v", ne.Name)
	for ind := range (*NodeResourceDb)[ne.Name] {
		fmt.Printf("%+v\n", (*NodeResourceDb)[ne.Name][ind])
		if (*NodeResourceDb)[ne.Name][ind].Resource == timerType {
			if (*NodeResourceDb)[ne.Name][ind].RemainingTime > 0 {
				log.Printf("Before: %v", (*NodeResourceDb)[ne.Name][ind].RemainingTime)
				(*NodeResourceDb)[ne.Name][ind].Key.Lock()
				(*NodeResourceDb)[ne.Name][ind].RemainingTime = -1
				(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
				log.Printf("After: %v", (*NodeResourceDb)[ne.Name][ind].RemainingTime)
			}
		} else {
			log.Printf("Event not found %v %v", (*NodeResourceDb)[ne.Name][ind].Name, timerType)
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

func AssesResult(config ioreader.Config, NodeResourceDb *map[string][]*CriticalNeCounter, ne *ioreader.Node, result ResourceUtil) []CriticalResource { //Per NE
	AnyCriticalFound := false

	critical := []CriticalResource{}
	_, NeAlreadyInDB := (*NodeResourceDb)[ne.Name]

	if result.Cpu >= ne.CpuThreshold {
		log.Printf("High cpu utilization for %v value %v", ne.Name, result.Cpu)
		AnyCriticalFound = true
		critical = append(critical, CriticalResource{
			Resource: "CPU",
			Value:    result.Cpu,
			ID:       result.ID,
		})
	} else {
		if NeAlreadyInDB {
			log.Println("Reseting for CPU")
			ResetTimer(NodeResourceDb, ne, "CPU")
		}
	}
	if result.Ram >= ne.RamThreshold {
		AnyCriticalFound = true
		critical = append(critical, CriticalResource{
			Resource: "RAM",
			Value:    result.Ram,
			ID:       result.ID,
		})

	} else {
		if NeAlreadyInDB {
			ResetTimer(NodeResourceDb, ne, "RAM")
		}
	}

	for mp, val := range result.Disk {
		if val >= ne.DiskThreshold {
			AnyCriticalFound = true
			critical = append(critical, CriticalResource{
				Resource: fmt.Sprintf("Disk: %v", mp),
				Value:    val,
				ID:       result.ID,
			})
		}
	}

	if AnyCriticalFound {
		if !NeAlreadyInDB {
			(*NodeResourceDb)[ne.Name] = []*CriticalNeCounter{}
		}

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
				go newNodeinDb.Wait30Min()
				(*NodeResourceDb)[ne.Name] = append((*NodeResourceDb)[ne.Name], &newNodeinDb)
			} else {
				for ind := range (*NodeResourceDb)[ne.Name] {
					if (*NodeResourceDb)[ne.Name][ind].Resource == critical[ind].Resource { //if the criticalrrsource in ne query c.Resource , already has a wait30min object (j.Resource).
						fmt.Println("FOUND EVENT!")
						log.Println((*NodeResourceDb)[ne.Name][ind].RemainingTime)
						if (*NodeResourceDb)[ne.Name][ind].RemainingTime < 0 {
							log.Printf("Starting timer = High value for %v - %v - %v", (*NodeResourceDb)[ne.Name][ind].Name, (*NodeResourceDb)[ne.Name][ind].Resource, (*NodeResourceDb)[ne.Name][ind].Value)
							(*NodeResourceDb)[ne.Name][ind].Key.Lock()
							(*NodeResourceDb)[ne.Name][ind].RemainingTime = config.RamCpuTimePeriod
							(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
							log.Printf("%v was -1, raising it!-Value: %v - Resource: %v, Time: %v", (*NodeResourceDb)[ne.Name][ind].Name, (*NodeResourceDb)[ne.Name][ind].Value, (*NodeResourceDb)[ne.Name][ind].Resource, (*NodeResourceDb)[ne.Name][ind].RemainingTime)
							go (*NodeResourceDb)[ne.Name][ind].Wait30Min()
						} else if (*NodeResourceDb)[ne.Name][ind].RemainingTime == 0 {
							log.Printf("Sending MAIL for %v", (*NodeResourceDb)[ne.Name][ind].Name)
							mailBody := []mail.MailBody{
								{
									Name:     (*NodeResourceDb)[ne.Name][ind].Name,
									Resource: (*NodeResourceDb)[ne.Name][ind].Resource,
									Value:    (*NodeResourceDb)[ne.Name][ind].Value,
								},
							}
							InitMail(config, mailBody)
							(*NodeResourceDb)[ne.Name][ind].Key.Lock()
							(*NodeResourceDb)[ne.Name][ind].RemainingTime = config.RamCpuTimePeriod
							(*NodeResourceDb)[ne.Name][ind].Key.Unlock()
							log.Printf("%v is 0,resetting timer after mail is sent! ---- Value: %v - Resource: %v, Time: %v", (*NodeResourceDb)[ne.Name][ind].Name, (*NodeResourceDb)[ne.Name][ind].Value, (*NodeResourceDb)[ne.Name][ind].Resource, (*NodeResourceDb)[ne.Name][ind].RemainingTime)
							go (*NodeResourceDb)[ne.Name][ind].Wait30Min()
						}
						break
					}
				}
			}
		}
		return critical
	} else {
		return nil
	}
}
