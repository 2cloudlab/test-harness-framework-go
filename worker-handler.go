package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	lambda_context "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
)

type Performancer interface {
	Init()
	Start(ctx context.Context, params EventParams) map[string][]float64
}

var performer *Performancer

func Record(key string, value []byte) {
	input := &s3.PutObjectInput{
		Body:   bytes.NewReader(value),
		Bucket: aws.String(os.Getenv("BUCKET_NAME")),
		Key:    aws.String(key),
	}

	_, err := g_s3_service.PutObject(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			fmt.Println(err.Error())
		}
		return
	}
}

var performers = map[string]*Performancer{}
var classes = map[string]func() Performancer{}

func registerPerformancer(name string, f func() Performancer) {
	if _, ok := classes[name]; ok {
		return
	}

	classes[name] = f
}

func registerAll() {
	registerPerformancer("DefaultPerformancer", func() Performancer {
		return DefaultPerformancer{}
	})
	registerPerformancer("S3Performancer", func() Performancer {
		return S3Performancer{}
	})
}

func getPerformancer(name string) *Performancer {
	if val, ok := performers[name]; ok {
		return val
	}
	tmp := classes[name]()
	tmp.Init()
	performers[name] = &tmp
	return performers[name]
}

func LambdaHandler(ctx context.Context, params EventParams) (int, error) {
	performer = getPerformancer(params.TaskName)
	lc, _ := lambdacontext.FromContext(ctx)
	r, err := json.Marshal((*performer).Start(ctx, params))
	if err == nil {
		Record(getReportName(params.RequestID, lc.AwsRequestID), r)
	}
	return 0, nil
}

func main() {
	fmt.Println("Init performancer for the first time")
	init_shared_resource()
	registerAll()
	lambda_context.Start(LambdaHandler)
}
