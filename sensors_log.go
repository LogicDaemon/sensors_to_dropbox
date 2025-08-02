package main

/*
#cgo LDFLAGS: -lsensors
#include <sensors/sensors.h>
#include <stdlib.h>

// Explicitly declare the functions for cgo
extern int sensors_init(FILE *input);
extern const sensors_chip_name *sensors_get_detected_chips(const sensors_chip_name *match, int *nr);
extern const sensors_feature *sensors_get_features(const sensors_chip_name *chip, int *nr);
extern char *sensors_get_label(const sensors_chip_name *chip, const sensors_feature *feature);
extern int sensors_get_value(const sensors_chip_name *chip, int subfeat_nr, double *value);
*/
import "C"
import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
)

const csvFilename = "sensor_data.csv"

type SensorDataProvider interface {
	Init(ctx context.Context) context.Context
	Write(ctx context.Context, data map[string]interface{})
}

type SlogProvider struct{}

func (p *SlogProvider) Init(ctx context.Context) context.Context {
	return ctx // no state needed
}

func (p *SlogProvider) Write(ctx context.Context, data map[string]interface{}) {
	slog.Info("Appending sensor", slog.Any("data", data))
}

type CsvProvider struct{}

// key for context value
var csvFileKey = struct{}{}

func (p *CsvProvider) Init(ctx context.Context) context.Context {
	file, err := os.OpenFile(csvFilename, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		slog.Error("Failed to open CSV file in init", slog.String("error", err.Error()))
		panic(err)
	}
	return context.WithValue(ctx, csvFileKey, file)
}

func (p *CsvProvider) Write(ctx context.Context, data map[string]interface{}) {
	file, ok := ctx.Value(csvFileKey).(*os.File)
	if !ok {
		panic("CSV file handle is not initialized in context")
	}
	if file == nil {
		panic("CSV file handle is nil")
	}
	// Prepare sorted keys for header and row
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	file.Seek(0, 0)
	scanner := bufio.NewScanner(file)
	header := []string{}
	var headerLine string
	var offset int64 = 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "timestamp,") {
			headerLine = line
			header = strings.Split(line, ",")[1:]
			break
		}
		offset += int64(len(line)) + 1 // +1 for newline
	}
	if len(header) == 0 {
		header = keys
	}

	// Compare header and keys
	keysMatch := len(header) == len(keys)
	if keysMatch {
		for i, k := range header {
			if k != keys[i] {
				keysMatch = false
				break
			}
		}
	}

	file.Seek(0, 2)

	if offset > 0 && (!keysMatch || len(headerLine) == 0) {
		file.WriteString("\n") // blank line before new header
	}
	if !keysMatch || len(headerLine) == 0 {
		headerLine := "timestamp"
		for _, k := range keys {
			headerLine += "," + k
		}
		headerLine += "\n"
		file.WriteString(headerLine)
		header = keys
	}

	timestamp := time.Now().Format(time.RFC3339)
	row := timestamp
	for _, k := range header {
		row += fmt.Sprintf(",%v", data[k])
	}
	row += "\n"
	if _, err := file.WriteString(row); err != nil {
		slog.Error("Failed to write to CSV file", slog.String("error", err.Error()))
	}
}

var sensorDataProviders = []SensorDataProvider{
	&SlogProvider{},
	&CsvProvider{},
}

func InitSensors() error {
	if C.sensors_init(nil) != 0 {
		return fmt.Errorf("failed to initialize sensors")
	}
	return nil
}

func FetchSensorReadings() map[string]interface{} {
	result := make(map[string]interface{})
	var chipNr C.int = 0
	for {
		chip := C.sensors_get_detected_chips(nil, &chipNr)
		if chip == nil {
			break
		}
		var featNr C.int = 0
		for {
			feature := C.sensors_get_features(chip, &featNr)
			if feature == nil {
				break
			}
			name := C.sensors_get_label(chip, feature)
			if name == nil {
				featNr++
				continue
			}
			label := C.GoString(name)
			var value C.double
			if C.sensors_get_value(chip, feature.number, &value) == 0 {
				result[label] = float64(value)
			}
			featNr++
		}
		chipNr++
	}
	return result
}

func InitWriters(ctx context.Context) map[SensorDataProvider]context.Context {
	providerContexts := make(map[SensorDataProvider]context.Context)
	for _, provider := range sensorDataProviders {
		providerContexts[provider] = provider.Init(ctx)
	}
	return providerContexts
}

func AppendSensorData(writersCtxs map[SensorDataProvider]context.Context, data map[string]interface{}) {
	for provider, pctx := range writersCtxs {
		provider.Write(pctx, data)
	}
}

func main() {
	period := 30 * time.Second

	if err := InitSensors(); err != nil {
		slog.Error("Failed to initialize sensors", slog.String("error", err.Error()))
		panic(err)
	}

	ctx := context.Background()
	writersCtxs := InitWriters(ctx)

	loopStart := time.Now()
	for {
		AppendSensorData(writersCtxs, FetchSensorReadings())
		time.Sleep(period - time.Since(loopStart))
		loopStart.Add(period)
		// Reset loopStart if the period has exceeded
		// to avoid repeatedly logging after a system sleep
		if time.Since(loopStart) > period {
			loopStart = time.Now()
		}
	}
}
