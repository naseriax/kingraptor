package ioreader

import (
	"encoding/csv"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"strconv"
)

type Config struct {
	MailRelayIp      string  `json:"mailRelayIp"`
	EnableMail       bool    `json:"enableMail"`
	RamCpuTimePeriod int     `json:"ramCpuTimePeriod"`
	MailFrom         string  `json:"mailFrom"`
	MailTo           string  `json:"mailTo"`
	MailInterval     int     `json:"mailInterval"`
	LogfileSize      float64 `json:"logSize"`
	QueryInterval    int     `json:"queryInterval"`
	WorkerQuantity   int     `json:"workerQuantity"`
	InputFileName    string  `json:"InputFileName"`
	Verbose          bool    `json:"verbose"`
	CycleQuantity    int
	Logging          bool `json:"loggin"`
}

type Node struct {
	IpAddress     string
	Name          string
	Username      string
	Password      string
	CpuThreshold  float64
	RamThreshold  float64
	DiskThreshold float64
	SshPort       string
}

func ParseCSV(csvdata [][]string) map[string]Node {
	nodes := map[string]Node{}
	floatVals := [3]float64{}

	for _, row := range csvdata[1:] {
		var err error
		for i := range floatVals {
			floatVals[i], err = strconv.ParseFloat(row[i+4], 64)
			if err != nil {
				log.Printf("Parse Error !%v", err.Error())
				floatVals[i] = 99
			}
		}
		tmp := Node{
			IpAddress:     row[0],
			Name:          row[1],
			Username:      row[2],
			Password:      row[3],
			CpuThreshold:  floatVals[0],
			RamThreshold:  floatVals[1],
			DiskThreshold: floatVals[2],
			SshPort:       row[7],
		}
		nodes[tmp.Name] = tmp
	}
	return nodes
}

func ReadCsvFile(filePath string) [][]string {
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatal("Unable to read input file "+filePath, err)
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	records, err := csvReader.ReadAll()
	if err != nil {
		log.Fatal("Unable to parse file as CSV for "+filePath, err)
	}
	return records
}

func ReadConfig(filePath string) Config {
	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatal("Unable to read the config file "+filePath, err)
	}

	config := Config{}
	_ = json.Unmarshal([]byte(file), &config)

	return config
}

func LoadNodes(filename string) map[string]Node {
	records := ReadCsvFile(filename)
	nodeList := ParseCSV(records)
	return nodeList
}
