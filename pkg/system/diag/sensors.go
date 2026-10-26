package diag

import esphome "github.com/ygelfand/go-esphome-device"

func (d *Diag) build() {
	for _, z := range d.o.Zones {
		d.zones[z.ID] = &esphome.Sensor{
			Base:        base(z.ID, z.Name, "mdi:thermometer"),
			Unit:        "°C",
			DeviceClass: "temperature",
			StateClass:  esphome.StateClassMeasurement,
			Decimals:    1,
		}
	}
	count := func(id, name string) *esphome.Sensor {
		return &esphome.Sensor{
			Base:       base(id, name, "mdi:cpu-64-bit"),
			StateClass: esphome.StateClassMeasurement,
		}
	}
	d.cores = count("cpu_cores", "CPU cores")
	d.coresOnline = count("cpu_cores_online", "CPU cores online")
	d.usage = &esphome.Sensor{
		Base:       base("cpu_usage", "CPU usage", "mdi:chip"),
		Unit:       "%",
		StateClass: esphome.StateClassMeasurement,
		Decimals:   1,
	}
	d.load = &esphome.Sensor{
		Base:       base("load_average", "Load average", "mdi:gauge"),
		StateClass: esphome.StateClassMeasurement,
		Decimals:   2,
	}
	size := func(id, name, icon string) *esphome.Sensor {
		return &esphome.Sensor{
			Base:        base(id, name, icon),
			Unit:        "kB",
			DeviceClass: "data_size",
			StateClass:  esphome.StateClassMeasurement,
		}
	}
	d.memory = size("memory_available", "Memory available", "mdi:memory")
	d.free = size("free_space", "Free space", "mdi:harddisk")
	d.uptime = &esphome.Sensor{
		Base:        base("uptime", "Uptime", "mdi:timer-outline"),
		Unit:        "s",
		DeviceClass: "duration",
		StateClass:  esphome.StateClassTotalIncreasing,
	}
	d.signal = &esphome.Sensor{
		Base:        base("wifi_signal", "Wi-Fi signal", "mdi:wifi"),
		Unit:        "dBm",
		DeviceClass: "signal_strength",
		StateClass:  esphome.StateClassMeasurement,
	}
	rate := func(id, name, icon string) *esphome.Sensor {
		return &esphome.Sensor{
			Base:       base(id, name, icon),
			Unit:       "kB/s",
			StateClass: esphome.StateClassMeasurement,
			Decimals:   1,
		}
	}
	d.rxRate = rate("wifi_received", "Wi-Fi received", "mdi:download-network")
	d.txRate = rate("wifi_sent", "Wi-Fi sent", "mdi:upload-network")
	d.address = &esphome.TextSensor{Base: base("ip_address", "IP address", "mdi:ip-network")}
	d.network = &esphome.TextSensor{
		Base: base("wifi_network", "Wi-Fi network", "mdi:access-point-network"),
	}
	d.version = &esphome.TextSensor{Base: base("version", "Version", "mdi:tag-outline")}
	d.version.Set(d.o.Version)
}

func base(id, name, icon string) esphome.Base {
	return esphome.Base{ObjectID: id, Name: name, Icon: icon, Category: esphome.CategoryDiagnostic}
}
