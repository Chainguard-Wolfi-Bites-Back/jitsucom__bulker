package file_storage

import (
	"errors"
	"fmt"
	"github.com/jitsucom/bulker/base/utils"
	"github.com/jitsucom/bulker/bulker"
	"github.com/jitsucom/bulker/implementations"
)

const GCSBulkerTypeId = "gcs"
const GCSAutocommitUnsupported = "Stream mode is not supported for GCS. Please use 'batch' mode"

func init() {
	bulker.RegisterBulker(GCSBulkerTypeId, NewGCSBulker)
}

type GCSConfig struct {
	implementations.FileConfig `mapstructure:",squash" json:",inline" yaml:",inline"`
	Bucket                     string `mapstructure:"bucket,omitempty" json:"bucket,omitempty" yaml:"bucket,omitempty"`
	AccessKey                  any    `mapstructure:"accessKey,omitempty" json:"accessKey,omitempty" yaml:"accessKey,omitempty"`
}
type GCSBulker struct {
	implementations.GoogleCloudStorage
}

func NewGCSBulker(bulkerConfig bulker.Config) (bulker.Bulker, error) {
	gcsConfig := &GCSConfig{}
	if err := utils.ParseObject(bulkerConfig.DestinationConfig, gcsConfig); err != nil {
		return nil, fmt.Errorf("failed to parse destination config: %w", err)
	}
	googleConfig := implementations.GoogleConfig{
		FileConfig: gcsConfig.FileConfig,
		Bucket:     gcsConfig.Bucket,
		KeyFile:    gcsConfig.AccessKey,
	}
	//TODO: auto recoonect the same way as in SQL bulkers
	gcsAdapter, err := implementations.NewGoogleCloudStorage(&googleConfig)
	if err != nil {
		return nil, err
	}
	return &GCSBulker{*gcsAdapter}, nil
}

func (gcs *GCSBulker) CreateStream(id, tableName string, mode bulker.BulkMode, streamOptions ...bulker.StreamOption) (bulker.BulkerStream, error) {
	switch mode {
	case bulker.Stream:
		return nil, errors.New(GCSAutocommitUnsupported)
	case bulker.Batch:
		return NewTransactionalStream(id, gcs, tableName, streamOptions...)
	case bulker.ReplaceTable:
		return NewReplaceTableStream(id, gcs, tableName, streamOptions...)
	case bulker.ReplacePartition:
		return NewReplacePartitionStream(id, gcs, tableName, streamOptions...)
	}
	return nil, fmt.Errorf("unsupported bulk mode: %s", mode)
}