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
	"fmt"
	"log/slog"
	"time"
)

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

func AppendSensorData(data map[string]interface{}) {
	// Simulate appending sensor data to a log or database
	slog.Info("Appending sensor data", slog.Any("data", data))
}

func main() {
	period := 30 * time.Second

	if err := InitSensors(); err != nil {
		slog.Error("Failed to initialize sensors", slog.String("error", err.Error()))
		panic(err)
	}

	loopStart := time.Now()
	for {
		AppendSensorData(FetchSensorReadings())
		time.Sleep(period - time.Since(loopStart))
		loopStart.Add(period)
		// Reset loopStart if the period has exceeded
		// to avoid repeatedly logging after a system sleep
		if time.Since(loopStart) > period {
			loopStart = time.Now()
		}
	}
}
