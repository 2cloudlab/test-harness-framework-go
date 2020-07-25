package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/montanaflynn/stats"
)

func upload() {
	// generate 16 objects with size range from 1KB to 32 MB, increase by a factor of 2
	initSizeInBytes := 1024
	for i := 0; i < 16; i++ {
		subKey := getObjectName(i + 1)
		_, err := g_s3_service.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(g_bucket_name),
			Key:    aws.String(subKey),
		})

		if err != nil {
			//could not find object
			input := &s3.PutObjectInput{
				Body:   bytes.NewReader(make([]byte, initSizeInBytes)),
				Bucket: aws.String(g_bucket_name),
				Key:    aws.String(subKey),
			}
			_, err := g_s3_service.PutObject(input)

			if err != nil {
				recordError(err)
			}
		}
		initSizeInBytes *= 2
	}
}

func generate_report(prefix []byte) {
	// get report units from S3
	prefixInStr := strings.Trim(string(prefix[:]), "\"")
	fmt.Printf("get report units from S3, key is %s ...", prefixInStr)
	report_units := downloadByPrefix(g_bucket_name, prefixInStr)
	if len(report_units) == 0 {
		return
	}

	// parse headers
	fmt.Println("parse headers ...")
	headersToMap := map[string][]float64{}
	headers := []string{}
	m := []interface{}{}
	err := json.Unmarshal(report_units[0], &m)
	if err != nil {
		recordError(err)
		return
	}
	for key, _ := range m[0].(map[string]interface{}) {
		headersToMap[key] = []float64{}
		headers = append(headers, key)
	}

	// aggregate all report units
	fmt.Println("aggregate all report units ...")
	for _, item := range report_units {
		m = []interface{}{}
		err := json.Unmarshal(item, &m)
		if err != nil {
			recordError(err)
		}

		for _, obj := range m {
			for key, val := range obj.(map[string]interface{}) {
				headersToMap[key] = append(headersToMap[key], val.(float64))
			}
		}
	}

	// sort each metrics in descending order
	fmt.Println("sort each metrics in descending order ...")
	for _, key := range headers {
		sort.Sort(sort.Reverse(sort.Float64Slice(headersToMap[key])))
	}

	// do stats such as mean, p99, min etc.
	fmt.Println("do stats such as mean, p99, min etc. ...")
	var buffer strings.Builder
	flat_data := []float64{}
	record_number := 0
	headers_number := len(headers)
	for _, key := range headers {
		avg, _ := stats.Mean(headersToMap[key])
		min := headersToMap[key][len(headersToMap[key])-1]
		p25, _ := stats.Percentile(headersToMap[key], 25)
		p50, _ := stats.Percentile(headersToMap[key], 50)
		p75, _ := stats.Percentile(headersToMap[key], 75)
		p90, _ := stats.Percentile(headersToMap[key], 90)
		p99, _ := stats.Percentile(headersToMap[key], 99)
		max := headersToMap[key][0]
		single_metric_stats_header := fmt.Sprintf("|%s %s %s %s %s %s %s %s", "avg", "min", "p25", "p50", "p75", "p90", "p99", "max")
		buffer.WriteString(single_metric_stats_header)
		single_metric_stats := fmt.Sprintf("|%s %s %s %s %s %s %s %s", avg, min, p25, p50, p75, p90, p99, max)
		buffer.WriteString(single_metric_stats)
		flat_data = append(flat_data, headersToMap[key]...)
		record_number = len(headersToMap[key])
	}

	// generate report
	fmt.Println("generate report ...")

	buffer.WriteString(strings.Join(headers[:], " "))
	buffer.WriteString("\n")
	for i := 0; i < record_number; i++ {
		one_row := []float64{}
		for j := 0; j < headers_number; j++ {
			one_row = append(one_row, flat_data[i+j*record_number])
		}
		buffer.WriteString(strings.Trim(fmt.Sprint(one_row), "[]"))
		buffer.WriteString("\n")
	}

	d1 := []byte(strings.Trim(buffer.String(), "\n"))
	ioutil.WriteFile(fmt.Sprintf("report-%s.csv", prefixInStr), d1, 0644)
}

var g_bucket_name string

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Please provide bucket name, for example, enter the following command:")
		fmt.Println("./auto-run <your-bucket-name>")
		return
	}
	g_bucket_name = os.Args[1]
	init_shared_resource()
	// upload data to S3
	upload()
	// launch Lambda Function
	params := []EventParams{
		EventParams{
			Iteration:             5,
			LambdaFunctionName:    "worker-handler",
			ProfileName:           "S3Performancer",
			CountInSingleInstance: 2,
			RawJson:               `{ "FileSize" : 1}`,
		},
		//EventParams{Iteration: 10, LambdaFunctionName: "worker-handler", ProfileName: "EmptyPerformancer", CountInSingleInstance: 1},
	}
	fmt.Println("Start ...")
	results := [][]byte{}
	for _, p := range params {
		payLoadInJson, _ := json.Marshal(p)
		input := &lambda.InvokeInput{
			FunctionName: aws.String("test-harness-framework"),
			Payload:      payLoadInJson,
		}
		result, err := g_lambda_service.Invoke(input)
		if err != nil {
			recordError(err)
			continue
		}
		fmt.Println(string(result.Payload[:]))
		results = append(results, result.Payload)
	}

	// wait 15 minutes
	time.Sleep(1 * time.Minute)

	// generate report
	for _, item := range results {
		generate_report(item)
	}
	fmt.Println("End ...")
}