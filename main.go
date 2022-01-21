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

func PrepareWorkers(isClosed chan bool, config ioreader.Config, nelogin, nelogout chan string, workers int, jobs chan ioreader.Node, results chan retriever.ResourceUtil) {
	for i := 1; i <= workers; i++ {
		go retriever.DoQuery(isClosed, config, nelogin, nelogout, jobs, results)
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
		case <-time.After(25 * time.Second):
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

func DoCollect(isClosed chan bool, nelogin, nelogout chan string, NodeResourceDb *map[string][]*retriever.CriticalNeCounter, DiskMaildB *map[string][]*retriever.DiskMailedObjects, config ioreader.Config, jobs chan ioreader.Node, results chan retriever.ResourceUtil, Nodes map[string]ioreader.Node) {
	PrepareWorkers(isClosed, config, nelogin, nelogout, config.WorkerQuantity, jobs, results)
	AssignJobs(jobs, Nodes)
	res := ProcessResults(config, NodeResourceDb, Nodes, results)
	if res == nil {
		return
	}
	if config.EnableMail {
		mailBuffer := MakeMailBuffer(DiskMaildB, res, config.MailInterval)
		if len(mailBuffer) > 0 {
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

func MakeMailBuffer(DiskMaildB *map[string][]*retriever.DiskMailedObjects, res map[string][]*retriever.CriticalResource, mailInterval int) []mail.MailBody {
	mailBuffer := []mail.MailBody{}
	for name, resource := range res {
		for _, disk := range resource {
			if strings.Contains(disk.Resource, "Disk") {
				if _, IsNeExistsInMailDb := (*DiskMaildB)[name]; !IsNeExistsInMailDb {
					(*DiskMaildB)[name] = []*retriever.DiskMailedObjects{}
				}
				foundRecord := false
				for ind := range (*DiskMaildB)[name] {
					if (*DiskMaildB)[name][ind].Resource == disk.Resource {
						foundRecord = true
						if !(*DiskMaildB)[name][ind].Mailed {
							mailBuffer = append(mailBuffer, BuildMailBody(name, disk))
							go (*DiskMaildB)[name][ind].StartMailTimer(mailInterval)
						}
					}
				}
				if !foundRecord {
					mailDbEntry := BuildMailDbEntry(name, disk)
					go mailDbEntry.StartMailTimer(mailInterval)
					(*DiskMaildB)[name] = append((*DiskMaildB)[name], mailDbEntry)
					mailBuffer = append(mailBuffer, BuildMailBody(name, disk))
				}
			}
		}
	}
	return mailBuffer
}

func findIndex(list []string, item string) int {
	for i, val := range list {
		if val == item {
			return i
		}
	}
	return -1
}

func RemoveIndex(s []string, item string) []string {
	index := findIndex(s, item)
	if index != -1 {
		return append(s[:index], s[index+1:]...)
	} else {
		return s
	}
}

//closeGracefully receives the keyboard interrupt signal from os (CTRL-C) and initiates gracefull closure by waiting for session logouts to finish.
func closeGracefully(isClosed chan<- bool, nelogin, nelogout <-chan string, c chan os.Signal) {
	localSessionInfo := []string{}
	for {
		select {
		case ne := <-nelogin:
			localSessionInfo = append(localSessionInfo, ne)
		case ne := <-nelogout:
			localSessionInfo = RemoveIndex(localSessionInfo, ne)
		case <-c:
			fmt.Println("\nCTRL-C Detected!")
			if len(localSessionInfo) == 0 {
				log.Println("all session are terminated!")
				os.Exit(0)
				return
			} else {
				ClosureTimeout := 20
				for len(localSessionInfo) > 0 {
					if ClosureTimeout <= 0 {
						log.Println("closeure timeout")
						log.Println("below sessions are still open:")
						for _, n := range localSessionInfo {
							fmt.Println(n)
						}
						os.Exit(1)
						return
					}
					select {
					case ne := <-nelogout:
						localSessionInfo = RemoveIndex(localSessionInfo, ne)
					case ne := <-nelogin:
						localSessionInfo = append(localSessionInfo, ne)
					case <-time.After(time.Second):
						ClosureTimeout -= 1
						log.Printf("open sessions: %v", len(localSessionInfo))
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

	nelogin := make(chan string)
	nelogout := make(chan string)
	isClosed := make(chan bool, 1)

	go closeGracefully(isClosed, nelogin, nelogout, prepareOsSig())

	NodeResourceDb := map[string][]*retriever.CriticalNeCounter{}
	DiskMailDb := map[string][]*retriever.DiskMailedObjects{}

	for {
		config := LoadConfig(configFileName)
		config.WorkerQuantity = FixWorkerQuantity(config.WorkerQuantity, len(Nodes))

		jobs := make(chan ioreader.Node, len(Nodes))
		results := make(chan retriever.ResourceUtil, len(Nodes))

		DoCollect(isClosed, nelogin, nelogout, &NodeResourceDb, &DiskMailDb, config, jobs, results, Nodes)
		Wait(config.QueryInterval)
	}
}
