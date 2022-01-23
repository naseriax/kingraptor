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

var IsEnded = false

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

func DoCollect(NodeResourceDb *map[string][]*retriever.CriticalNeCounter, DiskMaildB *map[string][]*retriever.DiskMailedObjects, config ioreader.Config, results chan retriever.ResourceUtil, Nodes map[string]ioreader.Node) bool {
	workerpool := make(chan bool, config.WorkerQuantity)
	for _, ne := range Nodes {
		if IsEnded {
			return false
		}

		workerpool <- true
		go func(ne ioreader.Node) {
			defer func() { <-workerpool }()
			retriever.DoQuery(config, ne, results)
		}(ne)
	}

	res := ProcessResults(config, NodeResourceDb, Nodes, results)
	if res == nil {
		return true
	}

	if config.EnableMail {
		mailBuffer := MakeMailBuffer(DiskMaildB, res, config.MailInterval)
		if len(mailBuffer) > 0 {
			retriever.InitMail(config, mailBuffer)
		}
	}
	return true
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
func closeAll(c chan os.Signal) {
	<-c
	IsEnded = true
	fmt.Println("\nCTRL-C Detected!")
	os.Exit(0)
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

	go closeAll(prepareOsSig())

	NodeResourceDb := map[string][]*retriever.CriticalNeCounter{}
	DiskMailDb := map[string][]*retriever.DiskMailedObjects{}

	for {
		config := LoadConfig(configFileName)
		config.WorkerQuantity = FixWorkerQuantity(config.WorkerQuantity, len(Nodes))

		results := make(chan retriever.ResourceUtil, len(Nodes))

		if DoCollect(&NodeResourceDb, &DiskMailDb, config, results, Nodes) {
			Wait(config.QueryInterval)
		} else {
			log.Println("shutting down the engine")
			break
		}
	}
}
