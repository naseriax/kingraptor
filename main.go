package main

import (
	"kingraptor/pkgs/ioreader"
	"kingraptor/pkgs/retriever"
	"log"
	"path/filepath"
	"strings"
	"time"
)

func LoadConfig(configFileName string) ioreader.Config {
	configFilePath := filepath.Join("conf", configFileName)
	config := ioreader.ReadConfig(configFilePath)
	return config
}

func PrepareWorkers(workers int, jobs chan ioreader.Node, results chan retriever.ResourceUtil) {

	for i := 1; i <= workers; i++ {
		go retriever.DoQuery(jobs, results)
	}
}

func AssignJobs(jobs chan ioreader.Node, Nodes *map[string]ioreader.Node) {
	for _, ne := range *Nodes {
		jobs <- ne
	}
	close(jobs)
}

func ProcessResults(config ioreader.Config, NodeResourceDb *map[string][]*retriever.CriticalNeCounter, Nodes *map[string]ioreader.Node, results chan retriever.ResourceUtil) map[string][]*retriever.CriticalResource {
	CR_Resources := map[string][]*retriever.CriticalResource{}
	for _, ne := range *Nodes {
		res := <-results
		if res.IsCollected {
			c_metrics := retriever.AssesResult(config, NodeResourceDb, ne, res)
			if c_metrics == nil {
				return nil
			} else {
				CR_Resources[ne.Name] = c_metrics
			}
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

func DoCollect(NodeResourceDb *map[string][]*retriever.CriticalNeCounter, config ioreader.Config, jobs chan ioreader.Node, results chan retriever.ResourceUtil, Nodes *map[string]ioreader.Node) {
	PrepareWorkers(config.WorkerQuantity, jobs, results)
	AssignJobs(jobs, Nodes)
	res := ProcessResults(config, NodeResourceDb, Nodes, results)
	if res == nil {
		return
	}

	// mailBuffer :=
	for name, resource := range res {
		for _, disk := range resource {
			if strings.Contains(disk.Resource, "Disk") {
				retriever.InitMail(config, name, disk.Resource, "bulk", disk.Value)
			}
		}
	}
}

func main() {
	configFileName := "config2.json"
	config := LoadConfig(configFileName)
	Nodes := ioreader.LoadNodes(filepath.Join("input", config.InputFileName))

	NodeResourceDb := map[string][]*retriever.CriticalNeCounter{}

	for {
		config := LoadConfig(configFileName)
		config.WorkerQuantity = FixWorkerQuantity(config.WorkerQuantity, len(Nodes))

		jobs := make(chan ioreader.Node, len(Nodes))
		results := make(chan retriever.ResourceUtil, len(Nodes))

		DoCollect(&NodeResourceDb, config, jobs, results, &Nodes)

		Wait(config.QueryInterval)
	}
}
