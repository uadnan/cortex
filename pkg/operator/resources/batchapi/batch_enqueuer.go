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
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	awslib "github.com/cortexlabs/cortex/pkg/lib/aws"
	"github.com/cortexlabs/cortex/pkg/lib/errors"
)

const (
	_messageSizeLimit    = 256 * 1024
	_maxMessagesPerBatch = 10
)

type SQSBatchUploader struct {
	Client               *awslib.Client
	QueueURL             string
	Retries              *int // default 3 times
	messageList          []*sqs.SendMessageBatchRequestEntry
	messageIDToListIndex map[string]int
	totalBytes           int
	TotalBatches         int
}

func (uploader *SQSBatchUploader) AddToBatch(id string, body *string) error {
	if len(*body) > _messageSizeLimit {
		return ErrorMessageExceedsMaxSize(len(*body), _messageSizeLimit)
	}

	message := &sqs.SendMessageBatchRequestEntry{
		Id:                     aws.String(id),
		MessageBody:            body,
		MessageDeduplicationId: aws.String(id), // prevent content based deduping
		MessageGroupId:         aws.String(id), // aws recommends message group id per message to improve chances of exactly-once
	}

	// TODO test what happens if 2 separate message bodies add up to 256 KB
	if len(*message.MessageBody)+uploader.totalBytes > _messageSizeLimit || len(uploader.messageList) == _maxMessagesPerBatch {
		err := uploader.Flush()
		if err != nil {
			return err
		}
	}

	uploader.messageList = append(uploader.messageList, message)
	uploader.messageIDToListIndex[id] = uploader.TotalBatches
	uploader.totalBytes += len(*message.MessageBody)
	uploader.TotalBatches++
	return nil
}

func (uploader *SQSBatchUploader) Flush() error {
	if len(uploader.messageList) == 0 {
		return nil
	}

	retries := 3
	if uploader.Retries != nil {
		retries = *uploader.Retries
	}

	var err error

	for attempt := 0; attempt < retries; attempt++ {
		err = uploader.enqueueToSQS()
		if err == nil {
			uploader.messageList = nil
			uploader.messageIDToListIndex = map[string]int{}
			uploader.totalBytes = 0
			return nil
		}
	}
	return errors.Wrap(err, fmt.Sprintf("failed after retrying %d times", retries))
}

func (uploader *SQSBatchUploader) enqueueToSQS() error {
	output, err := uploader.Client.SQS().SendMessageBatch(&sqs.SendMessageBatchInput{
		QueueUrl: aws.String(uploader.QueueURL),
		Entries:  uploader.messageList,
	})
	if err != nil {
		if len(output.Failed) == 0 {
			return errors.WithStack(err)
		}

		return errors.Wrap(ErrorFailedToEnqueueMessages(*output.Failed[0].Message), fmt.Sprintf("batch %s", uploader.messageIDToListIndex[*output.Failed[0].Id]))
	}

	return nil
}
