package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

const (
	CriticalFanSpeed = 100
)

type Config struct {
	TimeToUpdate      int                `json:"time_to_update"`
	CriticalTemp      int                `json:"critical_temp,omitempty"`
	TemperatureRanges []TemperatureRange `json:"temperature_ranges"`

}

type TemperatureRange struct {
	MinTemperature int `json:"min_temperature"`
	MaxTemperature int `json:"max_temperature"`
	FanSpeed       int `json:"fan_speed"`
	Hysteresis     int `json:"hysteresis"`
}

func loadConfig(file string) (Config, error) {
	var config Config
	data, err := os.ReadFile(file)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(data, &config)
	return config, err
}

func getFanSpeedForTemperature(temp, prevSpeed int, ranges []TemperatureRange) int {
	if len(ranges) == 0 {
		return prevSpeed
	}

	// 1. Находим целевой диапазон для текущей температуры
	targetRangeIdx := -1
	for i, r := range ranges {
		// Используем >= для нижней границы, чтобы корректно обрабатывать 0°C и стыки
		if temp >= r.MinTemperature && (temp < r.MaxTemperature || i == len(ranges)-1) {
			targetRangeIdx = i
			break
		}
	}

	if targetRangeIdx == -1 {
		return prevSpeed // Температура вне заданных лимитов
	}

	// 2. Находим текущий диапазон, в котором мы находимся (по предыдущей скорости)
	currentRangeIdx := -1
	for i, r := range ranges {
		if prevSpeed == r.FanSpeed {
			currentRangeIdx = i
			break
		}
	}

	if currentRangeIdx == -1 || currentRangeIdx == targetRangeIdx {
		return ranges[targetRangeIdx].FanSpeed
	}

	currentRange := ranges[currentRangeIdx]

	// 3. Логика переключения с учетом гистерезиса
	if targetRangeIdx > currentRangeIdx {
		// НАГРЕВ: Переключаемся сразу, как только попали в новый диапазон
		// (Гистерезис при нагреве обычно не нужен, чтобы не перегревать карту)
		return ranges[targetRangeIdx].FanSpeed
	} else {
		// ОХЛАЖДЕНИЕ: Применяем гистерезис, чтобы избежать "дребезга" вентиляторов
		if temp <= currentRange.MinTemperature-currentRange.Hysteresis {
			return ranges[targetRangeIdx].FanSpeed
		}
	}

	return currentRange.FanSpeed
}

// RotatingFileWriter реализует io.Writer и автоматически ротирует логи
type RotatingFileWriter struct {
	mu          sync.Mutex
	filename    string
	maxSize     int64 // Максимальный размер файла в байтах
	maxBackups  int   // Сколько старых файлов хранить
	file        *os.File
	currentSize int64
}

// NewRotatingFileWriter создает и открывает writer с настройками ротации
func NewRotatingFileWriter(filename string, maxSizeMB int, maxBackups int) (*RotatingFileWriter, error) {
	w := &RotatingFileWriter{
		filename:   filename,
		maxSize:    int64(maxSizeMB) * 1024 * 1024, // Переводим МБ в байты
		maxBackups: maxBackups,
	}

	// Открываем файл при старте
	f, err := os.OpenFile(w.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	w.file = f
	w.currentSize = info.Size()

	return w, nil
}

// Write проверяет размер перед записью и вызывает ротацию при необходимости
func (w *RotatingFileWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, fmt.Errorf("log file is closed")
	}

	// Если добавление новых данных превысит лимит — ротируем
	if w.currentSize+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, fmt.Errorf("failed to rotate log: %w", err)
		}
	}

	n, err = w.file.Write(p)
	w.currentSize += int64(n)
	return n, err
}

// Close корректно закрывает файловый дескриптор
func (w *RotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// rotate сдвигает старые бэкапы и создает новый файл
func (w *RotatingFileWriter) rotate() error {
	// 1. Закрываем текущий файл
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}

	// 2. Сдвигаем старые бэкапы (например: .4 -> .5, .3 -> .4, .2 -> .3, .1 -> .2)
	for i := w.maxBackups - 1; i > 0; i-- {
		src := fmt.Sprintf("%s.%d", w.filename, i)
		dst := fmt.Sprintf("%s.%d", w.filename, i+1)
		os.Rename(src, dst) // Игнорируем ошибки, если файла не существует
	}

	// 3. Переименовываем текущий заполненный файл в бэкап .1
	if w.maxBackups > 0 {
		os.Rename(w.filename, fmt.Sprintf("%s.%d", w.filename, 1))
	} else {
		os.Remove(w.filename) // Если бэкапы не нужны, просто удаляем
	}

	// 4. Открываем новый пустой файл для записи
	f, err := os.OpenFile(w.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.currentSize = 0
	return nil
}

func setupLogging(logFilePath string) (*RotatingFileWriter, error) {
	// Настройки: Максимум 1 МБ на файл, хранить 1 старую копию
	writer, err := NewRotatingFileWriter(logFilePath, 1, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to init rotating logger: %w", err)
	}

	// Передаем наш Writer в стандартный пакет log
	log.SetOutput(writer)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("INFO: Logging setup complete with custom rotation.")

	return writer, nil
}

func loadConfiguration(configPath string) (Config, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return config, fmt.Errorf("failed to load config %s: %w", configPath, err)
	}

	if config.TimeToUpdate <= 0 {
		log.Printf("WARN: time_to_update (%d) is invalid, defaulting to 5 seconds.", config.TimeToUpdate)
		config.TimeToUpdate = 5
	}

	if len(config.TemperatureRanges) == 0 {
		return config, fmt.Errorf("temperature_ranges must not be empty")
	}

	for i, r := range config.TemperatureRanges {
		if r.MinTemperature >= r.MaxTemperature {
			return config, fmt.Errorf("range %d: min_temperature (%d) must be less than max_temperature (%d)", i, r.MinTemperature, r.MaxTemperature)
		}
		if r.FanSpeed < 0 || r.FanSpeed > 100 {
			return config, fmt.Errorf("range %d: fan_speed (%d) must be between 0 and 100", i, r.FanSpeed)
		}
		if r.Hysteresis < 0 {
			return config, fmt.Errorf("range %d: hysteresis (%d) must not be negative", i, r.Hysteresis)
		}
		if i > 0 {
			prev := config.TemperatureRanges[i-1]
			if r.MinTemperature != prev.MaxTemperature {
				return config, fmt.Errorf("range %d: min_temperature (%d) must equal previous range max_temperature (%d) to avoid gaps", i, r.MinTemperature, prev.MaxTemperature)
			}
		}
	}

	if config.CriticalTemp <= 0 {
		config.CriticalTemp = 105
		log.Printf("INFO: critical_temp not set, defaulting to %d°C.", config.CriticalTemp)
	}

	log.Println("INFO: Configuration loaded and validated.")
	return config, nil
}

func initializeNVML() (cleanupFunc func(), err error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("unable to initialize NVML: %v", nvml.ErrorString(ret))
	}

	cleanupFunc = func() {
		log.Println("INFO: Shutting down NVML...")
		ret := nvml.Shutdown()
		if ret != nvml.SUCCESS {
			log.Printf("ERROR: Unable to shutdown NVML cleanly: %v", nvml.ErrorString(ret))
		} else {
			log.Println("INFO: NVML Shutdown complete.")
		}
	}

	log.Println("INFO: NVML initialized successfully.")
	return cleanupFunc, nil
}

func initializeDevices() (count int, fanCounts []int, prevFanSpeeds [][]int, err error) {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return 0, nil, nil, fmt.Errorf("unable to get NVIDIA device count: %v", nvml.ErrorString(ret))
	}
	if count == 0 {
		return 0, nil, nil, fmt.Errorf("no NVIDIA devices found")
	}
	log.Printf("INFO: Found %d NVIDIA device(s).", count)

	fanCounts = make([]int, count)
	prevFanSpeeds = make([][]int, count)
	initializedDevices := 0

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			log.Printf("WARN: Unable to get handle for device %d: %v. Skipping device.", i, nvml.ErrorString(ret))
			fanCounts[i] = 0
			continue
		}

		var numFansInt int
		numFansInt, ret = nvml.DeviceGetNumFans(device)
		if ret != nvml.SUCCESS {
			log.Printf("WARN: Unable to get fan count for device %d: %v. Assuming 0 fans or fan control not supported.", i, nvml.ErrorString(ret))
			fanCounts[i] = 0
			continue
		}
		fanCounts[i] = numFansInt
		if fanCounts[i] <= 0 {
			log.Printf("INFO: Device %d reports %d controllable fans. Skipping fan initialization.", i, fanCounts[i])
			continue
		}

		log.Printf("INFO: Device %d has %d controllable fan(s). Initializing state and setting MANUAL policy.", i, fanCounts[i])
		prevFanSpeeds[i] = make([]int, fanCounts[i])

		var initialTemp int
		temp, ret := nvml.DeviceGetTemperature(device, nvml.TEMPERATURE_GPU)
		if ret == nvml.SUCCESS {
			initialTemp = int(temp)
		} else {
			log.Printf("WARN: Failed to get initial temperature for device %d: %v", i, nvml.ErrorString(ret))
			initialTemp = -1
		}

		// ✅ НОВОЕ: Устанавливаем MANUAL политику ОДИН РАЗ при инициализации
		allFansConfigurable := true
		for fanIdx := 0; fanIdx < fanCounts[i]; fanIdx++ {
			ret = nvml.DeviceSetFanControlPolicy(device, fanIdx, nvml.FAN_POLICY_MANUAL)
			if ret != nvml.SUCCESS {
				log.Printf("ERROR: Unable to set MANUAL fan control policy for GPU %d Fan %d: %v. Device will not be managed.",
					i, fanIdx, nvml.ErrorString(ret))
				allFansConfigurable = false
				break
			}
			log.Printf("INFO: GPU %d Fan %d set to MANUAL control policy (once at init).", i, fanIdx)
		}

		// Если политика MANUAL не установилась — пропускаем устройство
		if !allFansConfigurable {
			fanCounts[i] = 0
			prevFanSpeeds[i] = nil
			continue
		}

		// Читаем текущие скорости вентиляторов (после установки MANUAL политики)
		for fanIdx := 0; fanIdx < fanCounts[i]; fanIdx++ {
			speed, ret := nvml.DeviceGetFanSpeed_v2(device, fanIdx)
			if ret == nvml.SUCCESS {
				prevFanSpeeds[i][fanIdx] = int(speed)
			} else {
				speedLegacy, retLegacy := nvml.DeviceGetFanSpeed(device)
				if retLegacy == nvml.SUCCESS && fanIdx == 0 {
					log.Printf("WARN: Using legacy DeviceGetFanSpeed for initial speed for device %d Fan %d.", i, fanIdx)
					prevFanSpeeds[i][fanIdx] = int(speedLegacy)
				} else {
					log.Printf("WARN: Failed to get initial speed for device %d Fan %d using v2 (%v) or legacy (%v). Using 0.", i, fanIdx, nvml.ErrorString(ret), nvml.ErrorString(retLegacy))
					prevFanSpeeds[i][fanIdx] = 0
				}
			}
		}

		log.Printf("INFO: Initial state for device %d: Temp=%d°C, Fan Speeds=%v%%", i, initialTemp, prevFanSpeeds[i])
		initializedDevices++
	}

	if initializedDevices == 0 && count > 0 {
		return count, fanCounts, prevFanSpeeds, fmt.Errorf("found %d devices, but failed to initialize any for monitoring/control", count)
	}

	log.Printf("INFO: Device initialization complete. Monitoring %d devices.", initializedDevices)
	return count, fanCounts, prevFanSpeeds, nil
}

func resetFansToAuto(count int, fanCounts []int) {
	log.Println("INFO: Resetting all fans to automatic control...")
	for i := 0; i < count; i++ {
		if fanCounts[i] == 0 {
			continue
		}

		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			log.Printf("ERROR: Unable to get handle for device %d during reset: %v", i, nvml.ErrorString(ret))
			continue
		}

		for fanIdx := 0; fanIdx < fanCounts[i]; fanIdx++ {
			ret = nvml.DeviceSetFanControlPolicy(device, fanIdx, nvml.FAN_POLICY_TEMPERATURE_CONTINUOUS_SW)
			if ret != nvml.SUCCESS {
				log.Printf("ERROR: Unable to reset fan %d on GPU %d to auto: %v", fanIdx, i, nvml.ErrorString(ret))
			} else {
				log.Printf("INFO: Reset GPU %d Fan %d to automatic control", i, fanIdx)
			}
		}
	}
	log.Println("INFO: Fan reset complete.")
}

func handleCriticalTemperature(device nvml.Device, deviceIdx int, fanCount int, temp int, prevFanSpeeds []int) {
	log.Printf("CRITICAL: GPU %d temperature %d°C exceeds critical threshold! Activating emergency cooling.", deviceIdx, temp)
	for fanIdx := 0; fanIdx < fanCount; fanIdx++ {
		// ✅ Политика MANUAL уже установлена при инициализации — сразу выкручиваем на 100%
		ret := nvml.DeviceSetFanSpeed_v2(device, fanIdx, CriticalFanSpeed)
		if ret != nvml.SUCCESS {
			log.Printf("ERROR: Unable to set emergency fan speed for GPU %d Fan %d: %v", deviceIdx, fanIdx, nvml.ErrorString(ret))
		} else {
			log.Printf("CRITICAL: GPU %d Fan %d set to %d%% (emergency)", deviceIdx, fanIdx, CriticalFanSpeed)
			prevFanSpeeds[fanIdx] = CriticalFanSpeed
		}
	}
	killGPUProcesses(device, deviceIdx)
}

func killGPUProcesses(device nvml.Device, deviceIdx int) {
	var pidsToKill []uint32

	computeProcesses, ret := nvml.DeviceGetComputeRunningProcesses(device)
	if ret == nvml.SUCCESS {
		for _, proc := range computeProcesses {
			pidsToKill = append(pidsToKill, proc.Pid)
			log.Printf("CRITICAL: Found compute process PID %d on GPU %d", proc.Pid, deviceIdx)
		}
	} else if ret != nvml.ERROR_NOT_SUPPORTED {
		log.Printf("ERROR: Unable to get compute processes for GPU %d: %v", deviceIdx, nvml.ErrorString(ret))
	}

	graphicsProcesses, ret := nvml.DeviceGetGraphicsRunningProcesses(device)
	if ret == nvml.SUCCESS {
		for _, proc := range graphicsProcesses {
			alreadyAdded := false
			for _, pid := range pidsToKill {
				if pid == proc.Pid {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				pidsToKill = append(pidsToKill, proc.Pid)
				log.Printf("CRITICAL: Found graphics process PID %d on GPU %d", proc.Pid, deviceIdx)
			}
		}
	} else if ret != nvml.ERROR_NOT_SUPPORTED {
		log.Printf("ERROR: Unable to get graphics processes for GPU %d: %v", deviceIdx, nvml.ErrorString(ret))
	}

	if len(pidsToKill) == 0 {
		log.Printf("INFO: No processes found on GPU %d to terminate", deviceIdx)
		return
	}

	log.Printf("CRITICAL: Terminating %d process(es) on GPU %d", len(pidsToKill), deviceIdx)

	for _, pid := range pidsToKill {
		err := syscall.Kill(int(pid), syscall.SIGTERM)
		if err == nil {
		time.Sleep(2 * time.Second) // Даем время на graceful shutdown
		// Проверяем, жив ли процесс (сигнал 0)
		if syscall.Kill(int(pid), 0) == nil {
			syscall.Kill(int(pid), syscall.SIGKILL) // Убиваем принудительно
		}
		} else if err != syscall.ESRCH {
		log.Printf("ERROR: Failed to send SIGTERM to PID %d: %v", pid, err)
		}
	}
}

func processDevices(config Config, count int, fanCounts []int, prevFanSpeeds [][]int, forceUpdate bool) {
	for i := 0; i < count; i++ {
		if fanCounts[i] == 0 {
			continue
		}

		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			log.Printf("ERROR: Unable to get handle for device %d during update: %v. Skipping cycle for this device.", i, nvml.ErrorString(ret))
			continue
		}

		temp, ret := nvml.DeviceGetTemperature(device, nvml.TEMPERATURE_GPU)
		if ret != nvml.SUCCESS {
			log.Printf("ERROR: Unable to get temperature for device %d: %v. Skipping cycle for this device.", i, nvml.ErrorString(ret))
			continue
		}

		tempInt := int(temp)
		if tempInt >= config.CriticalTemp {
			handleCriticalTemperature(device, i, fanCounts[i], tempInt, prevFanSpeeds[i])
			continue
		}

		for fanIdx := 0; fanIdx < fanCounts[i]; fanIdx++ {
			newFanSpeed := getFanSpeedForTemperature(tempInt, prevFanSpeeds[i][fanIdx], config.TemperatureRanges)
			if forceUpdate || newFanSpeed != prevFanSpeeds[i][fanIdx] {
				// ✅ ПОЛИТИКА MANUAL уже установлена при инициализации — сразу меняем скорость
				ret = nvml.DeviceSetFanSpeed_v2(device, fanIdx, newFanSpeed)
				if ret != nvml.SUCCESS {
					log.Printf("ERROR: Unable to set fan speed for GPU %d Fan %d to %d%%: %v", i, fanIdx, newFanSpeed, nvml.ErrorString(ret))
					continue
				}
				log.Printf("INFO: Updated GPU %d Fan %d: Temp=%d°C, PrevSpeed=%d%%, NewSpeed=%d%%",
					i, fanIdx, tempInt, prevFanSpeeds[i][fanIdx], newFanSpeed)
				prevFanSpeeds[i][fanIdx] = newFanSpeed
			}
		}
	}
}

func runMonitoringLoop(config Config, count int, fanCounts []int, prevFanSpeeds [][]int, stopChan <-chan struct{}) {
	log.Println("INFO: Starting monitoring loop...")

	processDevices(config, count, fanCounts, prevFanSpeeds, true)

	ticker := time.NewTicker(time.Duration(config.TimeToUpdate) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			log.Println("INFO: Received shutdown signal, stopping monitoring loop...")
			return
		case <-ticker.C:
			processDevices(config, count, fanCounts, prevFanSpeeds, false)
		}
	}
}

func run() int {
	logFile, err := setupLogging("/var/log/nvidia_fan_control.log")
	if err != nil {
		log.Printf("FATAL: %v", err)
		return 1
	}
	defer logFile.Close()

	config, err := loadConfiguration("config.json")
	if err != nil {
		log.Printf("FATAL: %v", err)
		return 1
	}

	nvmlCleanup, err := initializeNVML()
	if err != nil {
		log.Printf("FATAL: %v", err)
		return 1
	}
	defer nvmlCleanup()

	count, fanCounts, prevFanSpeeds, err := initializeDevices()
	if err != nil {
		log.Printf("FATAL: %v", err)
		return 1
	}

	hasControllableFans := false
	for _, fc := range fanCounts {
		if fc > 0 {
			hasControllableFans = true
			break
		}
	}

	if !hasControllableFans {
		log.Println("INFO: No devices with controllable fans were found or initialized. Exiting.")
		return 0
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	stopChan := make(chan struct{})
	go func() {
		sig := <-sigChan
		log.Printf("INFO: Received signal %v, initiating graceful shutdown...", sig)
		close(stopChan)

		sig = <-sigChan
		log.Printf("INFO: Received second signal %v, forcing exit.", sig)
		os.Exit(1)
	}()

	runMonitoringLoop(config, count, fanCounts, prevFanSpeeds, stopChan)

	resetFansToAuto(count, fanCounts)

	log.Println("INFO: Graceful shutdown complete.")
	return 0
}

func main() {
	os.Exit(run())
}
