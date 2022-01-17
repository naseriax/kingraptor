package retriever

import (
	"fmt"
	"kingraptor/pkgs/ioreader"
	"kingraptor/pkgs/sshagent"
	"log"
	"strconv"
	"strings"
	"time"
)

type ResourceUtil struct {
	Cpu         float64
	Ram         float64
	Disk        map[string]float64
	Name        string
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
		log.Printf("ne not reachable - %v", neName)
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
		`sar 1 1 | grep Average | awk '{print ($3 + $5)}'`, //-- CPU Query
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

func AssesResult(nodes map[string]ioreader.Node, result ResourceUtil) {
	if result.Cpu >= nodes[result.Name].CpuThreshold {
		log.Printf("Crossed the cpu threshold in NE: %v - Current Value: %v, Threshold: %v\n", result.Name, result.Cpu, nodes[result.Name].CpuThreshold)
	}
	if result.Ram >= nodes[result.Name].RamThreshold {
		log.Printf("Crossed the ram  threshold in NE: %v - Current Value: %v, Threshold: %v\n", result.Name, result.Ram, nodes[result.Name].RamThreshold)
	}

	for mp, val := range result.Disk {
		if val >= nodes[result.Name].DiskThreshold {
			log.Printf("Crossed the disk threshold in NE: %v - for mountpoint %v- Current Value: %v, Threshold: %v\n", result.Name, mp, val, nodes[result.Name].DiskThreshold)
		}
	}
}
