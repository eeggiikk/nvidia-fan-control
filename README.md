
# Nvidia Fan Control 
A lightweight Linux utility for monitoring GPU temperatures and dynamically controlling NVIDIA GPU fan speeds using NVML.

Ахтунг! Вайбкодинг версия!
Тестируется на Debian-based sys arm64 v9.2 + NVIDIA A-Series
От исходной версии отличается ограничением размера лога, гистерезис работает только при снижении температуры, в конфигурацию вынесен параметр критической температуры, при которой все процессе в видеокарте убивается наглухо.

## Requirements
- NVIDIA GPUs with NVML support
- NVIDIA drivers 520 or higher
  
### Check Logs
```bash
sudo tail -f /var/log/nvidia_fan_control.log
```
