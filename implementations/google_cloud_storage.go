package implementations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/jitsucom/bulker/base/errorj"
	"github.com/jitsucom/bulker/base/logging"
	"github.com/jitsucom/bulker/base/timestamp"
	"github.com/jitsucom/bulker/base/utils"
	"github.com/jitsucom/bulker/types"
	jsoniter "github.com/json-iterator/go"
	"io"
	"strings"

	"go.uber.org/atomic"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

var ErrMalformedBQDataset = errors.New("bq_dataset must be alphanumeric (plus underscores) and must be at most 1024 characters long")

type GoogleConfig struct {
	Bucket      string                `mapstructure:"gcsBucket,omitempty" json:"gcsBucket,omitempty" yaml:"gcsBucket,omitempty"`
	Folder      string                `mapstructure:"folder,omitempty" json:"folder,omitempty" yaml:"folder,omitempty"`
	Project     string                `mapstructure:"project,omitempty" json:"project,omitempty" yaml:"project,omitempty"`
	Dataset     string                `mapstructure:"bqDataset,omitempty" json:"bqDataset,omitempty" yaml:"bqDataset,omitempty"`
	KeyFile     any                   `mapstructure:"keyFile,omitempty" json:"keyFile,omitempty" yaml:"keyFile,omitempty"`
	Format      types.FileFormat      `mapstructure:"format,omitempty" json:"format,omitempty" yaml:"format,omitempty"`
	Compression types.FileCompression `mapstructure:"compression,omitempty" json:"compression,omitempty" yaml:"compression,omitempty"`

	//will be set on validation
	Credentials option.ClientOption
}

func (gc *GoogleConfig) Validate() error {
	if gc == nil {
		return errors.New("Google config is required")
	}

	if gc.Dataset != "" {
		if len(gc.Dataset) > 1024 {
			return ErrMalformedBQDataset
		}

		//check symbols
		for _, symbol := range gc.Dataset {
			if symbol != '_' && !utils.IsLetterOrNumber(symbol) {
				return fmt.Errorf("%s: '%s'", ErrMalformedBQDataset.Error(), string(symbol))
			}
		}
	}
	switch gc.KeyFile.(type) {
	case map[string]any:
		keyFileObject := gc.KeyFile.(map[string]any)
		if len(keyFileObject) == 0 {
			return errors.New("Google keyFile is required parameter")
		}
		b, err := jsoniter.Marshal(keyFileObject)
		if err != nil {
			return fmt.Errorf("Malformed google keyFile: %v", err)
		}
		gc.Credentials = option.WithCredentialsJSON(b)
	case string:
		keyFile := gc.KeyFile.(string)
		if keyFile == "workload_identity" {
			return nil
		}
		if keyFile == "" {
			return errors.New("Google keyFile is required parameter")
		}
		if strings.Contains(keyFile, "{") {
			gc.Credentials = option.WithCredentialsJSON([]byte(keyFile))
		} else {
			gc.Credentials = option.WithCredentialsFile(keyFile)
		}
	default:
		return errors.New("Google key_file must be string or json object")
	}

	return nil
}

type GoogleCloudStorage struct {
	config *GoogleConfig
	client *storage.Client

	closed *atomic.Bool
}

func NewGoogleCloudStorage(config *GoogleConfig) (*GoogleCloudStorage, error) {
	var client *storage.Client
	var err error
	err = config.Validate()
	if config.Credentials == nil {
		client, err = storage.NewClient(context.Background())
	} else {
		client, err = storage.NewClient(context.Background(), config.Credentials)
	}
	if err != nil {
		return nil, fmt.Errorf("Error creating google cloud storage client: %v", err)
	}

	if config.Format == "" {
		config.Format = types.FileFormatNDJSON
	}

	return &GoogleCloudStorage{client: client, config: config, closed: atomic.NewBool(false)}, nil
}

func (gcs *GoogleCloudStorage) Format() types.FileFormat {
	return gcs.config.Format
}

func (gcs *GoogleCloudStorage) Compression() types.FileCompression {
	return gcs.config.Compression
}

func (gcs *GoogleCloudStorage) UploadBytes(fileName string, fileBytes []byte) error {
	return gcs.Upload(fileName, bytes.NewReader(fileBytes))
}

// UploadBytes creates named file on google cloud storage with payload
func (gcs *GoogleCloudStorage) Upload(fileName string, fileReader io.ReadSeeker) (err error) {
	fileName = gcs.Path(fileName)

	//panic handler
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while uploading file: %s to GCC project: %s bucket: %s dataset: %s : %v", fileName, gcs.config.Project, gcs.config.Bucket, gcs.config.Dataset, r)
			logging.SystemErrorf(err.Error())
		}
	}()
	if gcs.closed.Load() {
		return fmt.Errorf("attempt to use closed GoogleCloudStorage instance")
	}

	bucket := gcs.client.Bucket(gcs.config.Bucket)
	object := bucket.Object(fileName)
	w := object.NewWriter(context.Background())

	if _, err := io.Copy(w, fileReader); err != nil {
		return errorj.SaveOnStageError.Wrap(err, "failed to write file to google cloud storage").
			WithProperty(errorj.DBInfo, &types.ErrorPayload{
				Bucket:    gcs.config.Bucket,
				Statement: fmt.Sprintf("file: %s", fileName),
			})
	}

	if err := w.Close(); err != nil {
		return errorj.SaveOnStageError.Wrap(err, "failed to close google cloud writer").
			WithProperty(errorj.DBInfo, &types.ErrorPayload{
				Bucket:    gcs.config.Bucket,
				Statement: fmt.Sprintf("file: %s", fileName),
			})
	}
	metadata := storage.ObjectAttrsToUpdate{}
	if gcs.config.Compression == types.FileCompressionGZIP {
		metadata.ContentEncoding = "gzip"
	}
	if gcs.config.Format == types.FileFormatCSV {
		metadata.ContentType = "text/csv"
	} else if gcs.config.Format == types.FileFormatNDJSON || gcs.config.Format == types.FileFormatNDJSONFLAT {
		metadata.ContentType = "application/x-ndjson"
	}
	if _, err := object.Update(context.Background(), metadata); err != nil {
		return errorj.SaveOnStageError.Wrap(err, "failed to set Content-Type metadata").
			WithProperty(errorj.DBInfo, &types.ErrorPayload{
				Bucket:    gcs.config.Bucket,
				Statement: fmt.Sprintf("file: %s", fileName),
			})
	}

	return nil
}

// Download downloads file from google cloud storage bucket
func (gcs *GoogleCloudStorage) Download(key string) (fileBytes []byte, err error) {
	key = gcs.Path(key)
	//panic handler
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while downloading file: %s from GCC project: %s bucket: %s dataset: %s : %v", key, gcs.config.Project, gcs.config.Bucket, gcs.config.Dataset, r)
			logging.SystemErrorf(err.Error())
		}
	}()
	if gcs.closed.Load() {
		return nil, fmt.Errorf("attempt to use closed GoogleCloudStorage instance")
	}
	bucket := gcs.client.Bucket(gcs.config.Bucket)
	obj := bucket.Object(key)

	r, err := obj.NewReader(context.Background())
	if err != nil {
		return nil, errorj.SaveOnStageError.Wrap(err, "failed to create google cloud reader").
			WithProperty(errorj.DBInfo, &types.ErrorPayload{
				Bucket:    gcs.config.Bucket,
				Statement: fmt.Sprintf("file: %s", key),
			})
	}
	defer r.Close()

	fileBytes, err = io.ReadAll(r)
	if err != nil {
		return nil, errorj.SaveOnStageError.Wrap(err, "failed to read google cloud reader").
			WithProperty(errorj.DBInfo, &types.ErrorPayload{
				Bucket:    gcs.config.Bucket,
				Statement: fmt.Sprintf("file: %s", key),
			})
	}

	return fileBytes, nil
}

// DeleteObject deletes object from google cloud storage bucket
func (gcs *GoogleCloudStorage) DeleteObject(key string) (err error) {
	key = gcs.Path(key)
	//panic handler
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while deleting file: %s to GCC project: %s bucket: %s dataset: %s : %v", key, gcs.config.Project, gcs.config.Bucket, gcs.config.Dataset, r)
			logging.SystemErrorf(err.Error())
		}
	}()
	if gcs.closed.Load() {
		return fmt.Errorf("attempt to use closed GoogleCloudStorage instance")
	}
	bucket := gcs.client.Bucket(gcs.config.Bucket)
	obj := bucket.Object(key)

	if err := obj.Delete(context.Background()); err != nil {
		return errorj.SaveOnStageError.Wrap(err, "failed to delete from google cloud").
			WithProperty(errorj.DBInfo, &types.ErrorPayload{
				Bucket:    gcs.config.Bucket,
				Statement: fmt.Sprintf("file: %s", key),
			})
	}

	return nil
}

// ValidateWritePermission tries to create temporary file and remove it.
// returns nil if file creation was successful.
func (gcs *GoogleCloudStorage) ValidateWritePermission() error {
	filename := fmt.Sprintf("test_%v", timestamp.NowUTC())

	if err := gcs.UploadBytes(filename, []byte{}); err != nil {
		return err
	}

	if err := gcs.DeleteObject(filename); err != nil {
		logging.Warnf("Cannot remove object %q from Google Cloud Storage: %v", filename, err)
		// Suppressing error because we need to check only write permission
		// return err
	}

	return nil
}

// Close closes gcp client and returns err if occurred
func (gcs *GoogleCloudStorage) Close() error {
	gcs.closed.Store(true)
	return gcs.client.Close()
}

func (gcs *GoogleCloudStorage) Path(fileName string) string {
	if gcs.config.Folder != "" {
		return fmt.Sprintf("%s/%s", gcs.config.Folder, fileName)
	}
	return fileName
}
