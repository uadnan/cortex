/*
Copyright 2020 Cortex Labs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package batchapi

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/cortexlabs/cortex/pkg/lib/errors"
	libtime "github.com/cortexlabs/cortex/pkg/lib/time"
	"github.com/cortexlabs/cortex/pkg/operator/config"
	"github.com/cortexlabs/cortex/pkg/types/spec"
)

func logGroupNameForJob(jobKey spec.JobKey) string {
	return fmt.Sprintf("%s/%s.%s", config.Cluster.LogGroup, jobKey.APIName, jobKey.ID)
}

func createLogGroupForJob(jobKey spec.JobKey) error {
	tags := map[string]string{
		"apiName": jobKey.APIName,
		"jobID":   jobKey.ID,
	}

	for key, value := range config.Cluster.Tags {
		tags[key] = value
	}

	_, err := config.AWS.CloudWatchLogs().CreateLogGroup(&cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(logGroupNameForJob(jobKey)),
		Tags:         aws.StringMap(tags),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	_, err = config.AWS.CloudWatchLogs().CreateLogStream(&cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(logGroupNameForJob(jobKey)),
		LogStreamName: aws.String(_operatorService),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func writeToJobLogGroup(jobKey spec.JobKey, logLine string, logLines ...string) error {
	logStreams, err := config.AWS.CloudWatchLogs().DescribeLogStreams(&cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName:        aws.String(logGroupNameForJob(jobKey)),
		LogStreamNamePrefix: aws.String(_operatorService),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	if len(logStreams.LogStreams) == 0 {
		return errors.ErrorUnexpected(fmt.Sprintf("unable to find log stream named '%s' in log group %s", _operatorService, logGroupNameForJob(jobKey)))
	}

	logLines = append([]string{logLine}, logLines...)

	inputLogEvents := make([]*cloudwatchlogs.InputLogEvent, len(logLines))
	curTime := libtime.ToMillis(time.Now())
	for i, line := range logLines {
		jsonBytes, _ := json.Marshal(fluentdLog{Log: line})
		inputLogEvents[i] = &cloudwatchlogs.InputLogEvent{
			Message:   aws.String(string(jsonBytes)),
			Timestamp: aws.Int64(curTime),
		}
	}

	_, err = config.AWS.CloudWatchLogs().PutLogEvents(&cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String(logGroupNameForJob(jobKey)),
		LogStreamName: aws.String(_operatorService),
		LogEvents:     inputLogEvents,
		SequenceToken: logStreams.LogStreams[0].UploadSequenceToken,
	},
	)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}
