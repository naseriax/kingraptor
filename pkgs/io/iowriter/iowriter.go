package iowriter

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"
)

type Log struct {
	FileName  string
	FilePath  string
	SizeLimit float64
}

func (n *Log) verifyLogFilePath() error {
	if _, err := os.Stat(n.FilePath); err == nil {
		return nil
	} else if errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(n.FilePath, 0755)
		if err != nil {
			return err
		}
	} else {
		return err
	}

	return nil
}

func FormatEpoch(epoch int64) string {

	return fmt.Sprintf("%v", time.Unix(epoch, 0).Format("2006-01-02 15:04:05"))
}

func (n *Log) rotateLog() {
	n.FileName = fmt.Sprintf("log_%v.csv", time.Now().Unix())
}

func (n *Log) verifyLogFile() error {
	if n.FileName == "" {
		n.rotateLog()
	}
	if fi, err := os.Stat(filepath.Join(n.FilePath, n.FileName)); err == nil {
		if float64(fi.Size()/1000000) < n.SizeLimit {
			return nil
		} else {
			n.rotateLog()
			err := ioutil.WriteFile(filepath.Join(n.FilePath, n.FileName), []byte("Date and Time,NE Name,Resource Name,Utilisation(%)\n"), 0644)
			if err != nil {
				return err
			}
		}

	} else if errors.Is(err, os.ErrNotExist) {
		n.rotateLog()
		err := ioutil.WriteFile(filepath.Join(n.FilePath, n.FileName), []byte("Date and Time,NE Name,Resource Name,Utilisation(%)\n"), 0644)
		if err != nil {
			return err
		}
	} else {
		log.Println("failed to access the logfile")
		return err
	}

	return nil
}

func (n *Log) WriteLog(text string) error {

	err := n.verifyLogFilePath()
	if err != nil {
		return err
	}

	err = n.verifyLogFile()
	if err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(n.FilePath, n.FileName), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(text + "\n"); err != nil {
		return err
	}

	return nil
}
