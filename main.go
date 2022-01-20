package main

import (
	"fmt"
	"kingraptor/pkgs/ioreader"
	"kingraptor/pkgs/mail"
	"kingraptor/pkgs/retriever"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func LoadConfig(configFileName string) ioreader.Config {
	configFilePath := filepath.Join("conf", configFileName)
	config := ioreader.ReadConfig(configFilePath)
	return config
}

func PrepareWorkers(nelogin, nelogout chan int, workers int, jobs chan ioreader.Node, results chan retriever.ResourceUtil) {
	for i := 1; i <= workers; i++ {
		go retriever.DoQuery(nelogin, nelogout, jobs, results)
	}
}

func AssignJobs(jobs chan ioreader.Node, Nodes map[string]ioreader.Node) {
	for _, ne := range Nodes {
		jobs <- ne
	}
	close(jobs)
}

func ProcessResults(config ioreader.Config, NodeResourceDb *map[string][]*retriever.CriticalNeCounter, Nodes map[string]ioreader.Node, results <-chan retriever.ResourceUtil) map[string][]*retriever.CriticalResource {
	CR_Resources := map[string][]*retriever.CriticalResource{}
	for range Nodes {
		select {
		case res := <-results:
			if res.IsCollected {
				for _, ne := range Nodes {
					if ne.Name == res.Name {
						c_metrics := retriever.AssesResult(config, NodeResourceDb, &ne, res)
						if c_metrics != nil {
							CR_Resources[ne.Name] = c_metrics
						}
						break
					}
				}
			}
		case <-time.After(10 * time.Second):
			log.Println("Timeout")
		}
	}
	return CR_Resources
}

func Wait(interval int) {
	log.Printf("Sleeping for %d second(s)", interval)
	time.Sleep(time.Duration(interval) * time.Second)
}

func FixWorkerQuantity(totalWorkers int, totalNodes int) int {
	if totalWorkers > totalNodes {
		log.Printf("Total Workers: %d\n", totalNodes)
		return totalNodes
	}
	log.Printf("Total Workers: %d\n", totalWorkers)
	return totalWorkers
}

func DoCollect(nelogin, nelogout chan int, NodeResourceDb *map[string][]*retriever.CriticalNeCounter, DiskMaildB *map[string][]*retriever.DiskMailedObjects, config ioreader.Config, jobs chan ioreader.Node, results chan retriever.ResourceUtil, Nodes map[string]ioreader.Node) {
	PrepareWorkers(nelogin, nelogout, config.WorkerQuantity, jobs, results)
	AssignJobs(jobs, Nodes)
	res := ProcessResults(config, NodeResourceDb, Nodes, results)
	if res == nil {
		return
	}
	if config.EnableMail {
		mailBuffer := MakeMailBuffer(DiskMaildB, res, config.MailInterval)
		if len(mailBuffer) > 0 {
			log.Println("mailBuffer is not empty - sending mail")
			retriever.InitMail(config, mailBuffer)
		}
	}
}

func BuildMailBody(name string, disk *retriever.CriticalResource) mail.MailBody {
	return mail.MailBody{
		Name:     name,
		Resource: disk.Resource,
		Value:    disk.Value,
		ID:       disk.ID,
	}
}

func BuildMailDbEntry(name string, disk *retriever.CriticalResource) *retriever.DiskMailedObjects {
	return &retriever.DiskMailedObjects{
		Name:     name,
		Resource: disk.Resource,
		Value:    disk.Value,
		ID:       disk.ID,
		Mailed:   false,
	}
}

func IsDiskMailInDb(DiskDbContent []*retriever.DiskMailedObjects, disk *retriever.CriticalResource) int {
	for ind := range DiskDbContent {
		if DiskDbContent[ind].Resource == disk.Resource {
			return ind
		}
	}
	return -1
}

// func MakeMailBuffer(DiskMaildB *map[string][]*retriever.DiskMailedObjects, res map[string][]*retriever.CriticalResource, mailInterval int) []mail.MailBody {
// 	mailBuffer := []mail.MailBody{}
// 	for name, resource := range res {
// 		for _, disk := range resource {
// 			if strings.Contains(disk.Resource, "Disk") {
// 				if _, IsNeExistsInMailDb := (*DiskMaildB)[name]; !IsNeExistsInMailDb {
// 					(*DiskMaildB)[name] = []*retriever.DiskMailedObjects{}
// 				}
// 				isDiskMailInDb := IsDiskMailInDb((*DiskMaildB)[name], disk)
// 				if isDiskMailInDb > 0 {
// 					if (*DiskMaildB)[name][isDiskMailInDb].Mailed {
// 						continue
// 					} else {
// 						go (*DiskMaildB)[name][isDiskMailInDb].StartMailTimer(mailInterval)
// 						mailBuffer = append(mailBuffer, BuildMailBody(name, disk))
// 					}
// 				} else {
// 					dbEntry := BuildMailDbEntry(name, disk)
// 					go dbEntry.StartMailTimer(mailInterval)
// 					(*DiskMaildB)[name] = append((*DiskMaildB)[name], dbEntry)
// 					mailBuffer = append(mailBuffer, BuildMailBody(name, disk))
// 				}
// 			}
// 		}
// 	}
// 	return mailBuffer
// }

func MakeMailBuffer(DiskMaildB *map[string][]*retriever.DiskMailedObjects, res map[string][]*retriever.CriticalResource, mailInterval int) []mail.MailBody {
	mailBuffer := []mail.MailBody{}
	for name, resource := range res {
		for _, disk := range resource {
			if strings.Contains(disk.Resource, "Disk") {
				if _, IsNeExistsInMailDb := (*DiskMaildB)[name]; !IsNeExistsInMailDb {
					log.Printf("IsNeExistsInMailDb is false for %v - %v", name, disk.Resource)
					(*DiskMaildB)[name] = []*retriever.DiskMailedObjects{}
					// mailDbEntry := &retriever.DiskMailedObjects{
					// 	Name:     name,
					// 	Resource: disk.Resource,
					// 	Value:    disk.Value,
					// 	ID:       disk.ID,
					// 	Mailed:   false,
					// }
					// log.Printf("Starting mail wait for %v %v", name, disk.Resource)
					// go mailDbEntry.StartMailTimer(mailInterval)
					// (*DiskMaildB)[name] = append((*DiskMaildB)[name], mailDbEntry)
					// mailBuffer = append(mailBuffer, mail.MailBody{
					// 	Name:     name,
					// 	Resource: disk.Resource,
					// 	Value:    disk.Value,
					// 	ID:       disk.ID,
					// })
				} //else {
				log.Printf("Ne exists in mail dB %v", name)
				foundRecord := false
				for ind := range (*DiskMaildB)[name] {
					if (*DiskMaildB)[name][ind].Resource == disk.Resource {
						foundRecord = true
						log.Println("Found the mail event in maildb")
						if !(*DiskMaildB)[name][ind].Mailed {
							log.Println("Mailed is fales!")
							mailBuffer = append(mailBuffer, mail.MailBody{
								Name:     name,
								Resource: disk.Resource,
								Value:    disk.Value,
								ID:       disk.ID,
							})

							log.Print("Staring mail wait - 2")
							go (*DiskMaildB)[name][ind].StartMailTimer(mailInterval)
						} else {
							log.Println("Already Notified")
						}
						continue
					}

				}
				if !foundRecord {
					log.Println("Ne exists but entry no. creating new entry")
					mailDbEntry := &retriever.DiskMailedObjects{
						Name:     name,
						Resource: disk.Resource,
						Value:    disk.Value,
						ID:       disk.ID,
						Mailed:   false,
					}
					log.Printf("Starting mail wait for 222 %v %v", name, disk.Resource)
					go mailDbEntry.StartMailTimer(mailInterval)
					(*DiskMaildB)[name] = append((*DiskMaildB)[name], mailDbEntry)
					mailBuffer = append(mailBuffer, mail.MailBody{
						Name:     name,
						Resource: disk.Resource,
						Value:    disk.Value,
						ID:       disk.ID,
					})
				}

				// }
			}
		}
	}
	return mailBuffer
}

//closeGracefully receives the keyboard interrupt signal from os (CTRL-C) and initiates gracefull closure by waiting for session logouts to finish.
func closeGracefully(nelogin, nelogout <-chan int, c chan os.Signal) {
	localSessionInfo := 0
	for {
		select {
		case <-nelogin:
			localSessionInfo += 1
		case <-nelogout:
			localSessionInfo -= 1
		case <-c:
			fmt.Println("\nCTRL-C Detected!")
			if localSessionInfo == 0 {
				log.Println("all session are terminated!")
				os.Exit(0)
				return
			} else {
				for localSessionInfo > 0 {
					select {
					case <-nelogout:
						localSessionInfo -= 1
					case <-nelogin:
						localSessionInfo += 1
					case <-time.After(time.Second):
						log.Printf("open sessions: %v", localSessionInfo)
						continue
					}
				}
				log.Println("all sessions are terminated after waiting...!")
				os.Exit(0)
				return
			}
		}
	}
}

func prepareOsSig() chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	return c
}

func main() {
	configFileName := "config2.json"
	config := LoadConfig(configFileName)
	Nodes := ioreader.LoadNodes(filepath.Join("input", config.InputFileName))

	nelogin := make(chan int)
	nelogout := make(chan int)

	go closeGracefully(nelogin, nelogout, prepareOsSig())

	NodeResourceDb := map[string][]*retriever.CriticalNeCounter{}
	DiskMailDb := map[string][]*retriever.DiskMailedObjects{}

	for {
		config := LoadConfig(configFileName)
		config.WorkerQuantity = FixWorkerQuantity(config.WorkerQuantity, len(Nodes))

		jobs := make(chan ioreader.Node, len(Nodes))
		results := make(chan retriever.ResourceUtil, len(Nodes))

		DoCollect(nelogin, nelogout, &NodeResourceDb, &DiskMailDb, config, jobs, results, Nodes)
		Wait(config.QueryInterval)
	}
}
