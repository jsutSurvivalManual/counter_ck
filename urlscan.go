package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var targetPorts = []struct {
	port    int
	isHttps bool
}{
	{80, false}, {443, true}, {8080, false}, {8443, true}, {8000, false}, {1080, false},
}

var tokens = make(chan struct{}, 64)

func init() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	log.Println("Disable cert check.")
}

type hostResult struct {
	Host      string
	isSSHOpen bool
	isRDPOpen bool
	Results   []portResult
}

type portResult struct {
	Port        int
	Title       string
	Protocol    string
	Code        int
	BodySummary string
}

func checkTCPPort(host string, port int) bool {
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	if conn != nil {
		defer conn.Close()
		return true
	}
	return false
}

func checkPage(url, host string, port int) (portResult, error) {
	var result portResult = portResult{}
	if !checkTCPPort(host, port) {
		return portResult{}, fmt.Errorf("%s :port %d is closed", host, port)
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	req, err := client.Get(url)
	if err != nil {
		return portResult{}, err
	}
	defer req.Body.Close()
	doc, err := goquery.NewDocumentFromReader(req.Body)
	if err != nil {
		return portResult{}, err
	}

	doc.Find("title").Each(func(i int, s *goquery.Selection) {
		result.Title = s.Text()
	})
	doc.Find("body").Each(func(i int, s *goquery.Selection) {
		result.BodySummary = s.Text()
		result.BodySummary = strings.ReplaceAll(result.BodySummary, "\n", "")
		result.BodySummary = strings.ReplaceAll(result.BodySummary, "\t", "")
		result.BodySummary = strings.Join(strings.Fields(result.BodySummary), " ")
	})

	result.Code = req.StatusCode

	return result, nil
}

func spider(ipAddress string, errors chan<- error, results chan<- hostResult) {
	var host_result hostResult
	host_result.Host = ipAddress
	host_result.Results = []portResult{}
	tokens <- struct{}{}

	for _, port := range targetPorts {
		var targetURL string
		if port.isHttps {
			targetURL = fmt.Sprintf("https://%s:%d", ipAddress, port.port)
		} else {
			targetURL = fmt.Sprintf("http://%s:%d", ipAddress, port.port)
		}
		result, err := checkPage(targetURL, ipAddress, port.port)
		if err != nil {
			errors <- err
			continue
		}
		if targetURL[4] == 's' {
			result.Protocol = "https"
		} else {
			result.Protocol = "http"
		}
		result.Port = port.port
		host_result.Results = append(host_result.Results, result)
	}

	host_result.isSSHOpen = checkTCPPort(ipAddress, 22)
	host_result.isRDPOpen = checkTCPPort(ipAddress, 3389)
	<-tokens

	results <- host_result
}

func printResult(e hostResult, fp *os.File) {
	if len(e.Results) == 0 {
		fmt.Print(".")
		return
	}
	fmt.Println("\nResult:")
	fmt.Printf("\tHost: %s, ssh %t, rdp %t\n", e.Host, e.isSSHOpen, e.isRDPOpen)
	fmt.Printf("\tOpen:\n")
	for i, t := range e.Results {
		fmt.Printf("\t  [%d] port %d: %s\n", i, t.Port, t.Title)
	}
	json_str, err := json.Marshal(e)
	if err != nil {
		return
	}
	fp.WriteString(string(json_str))

}

func main() {

	var errChan = make(chan error)
	var resultChan = make(chan hostResult)

	fp_err, err := os.Create("errors")
	if err != nil {
		panic(err)
	}
	fp_result, err := os.Create("results")
	if err != nil {
		panic(err)
	}

	go func() {
		for i := 1; i < 255; i++ {
			for j := 1; j < 255; j++ {
				go spider(fmt.Sprintf("23.96.%d.%d", i, j), errChan, resultChan)
			}
		}
	}()

	for {
		select {
		case e := <-errChan:
			fp_err.WriteString(e.Error())
			fp_err.WriteString("\n")
		case r := <-resultChan:
			printResult(r, fp_result)
		}
	}
}
