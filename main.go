package main

import (
	"kingraptor/pkgs/ioreader"
	"kingraptor/pkgs/retriever"
	"log"
	"path/filepath"
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

func ProcessResults(Nodes *map[string]ioreader.Node, results chan retriever.ResourceUtil) {
	for range *Nodes {
		res := <-results
		if res.IsCollected {
			retriever.AssesResult(*Nodes, res)
		}
	}
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

func DoCollect(workerQuantity int, jobs chan ioreader.Node, results chan retriever.ResourceUtil, Nodes *map[string]ioreader.Node) {
	PrepareWorkers(workerQuantity, jobs, results)
	AssignJobs(jobs, Nodes)
	ProcessResults(Nodes, results)
}

func main() {
	configFileName := "config.json"
	config := LoadConfig(configFileName)
	Nodes := ioreader.LoadNodes(filepath.Join("input", config.InputFileName))

	for {
		config := LoadConfig(configFileName)
		config.WorkerQuantity = FixWorkerQuantity(config.WorkerQuantity, len(Nodes))

		jobs := make(chan ioreader.Node, len(Nodes))
		results := make(chan retriever.ResourceUtil, len(Nodes))

		DoCollect(config.WorkerQuantity, jobs, results, &Nodes)
		Wait(config.QueryInterval)
	}
}
