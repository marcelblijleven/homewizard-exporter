# homewizard_exporter

Prometheus exporter for HomeWizard Energy devices. It reads the local API of
every meter, socket, water meter and battery in the house and exposes them all
on one `/metrics` endpoint.

You give it a list of IP addresses. It works out the rest: which API version
each device speaks, what product it is, and which endpoints that product serves.

## Install

```bash
# Container
docker run -d --name homewizard_exporter -p 9833:9833 \
  -e HOMEWIZARD_DEVICES=192.168.1.10,192.168.1.11 \
  -v homewizard-tokens:/var/lib/homewizard_exporter \
  ghcr.io/marcelblijleven/homewizard_exporter:latest

# From source
go install github.com/marcelblijleven/homewizard_exporter/cmd/homewizard_exporter@latest
```

> mDNS names like `p1meter.local` will not resolve inside a container. Use the
> IP address, and give the device a DHCP reservation on your router.

### systemd

```bash
make build
sudo install -m0755 homewizard_exporter /usr/local/bin/homewizard_exporter

sudo install -m0644 contrib/homewizard_exporter.service /etc/systemd/system/
sudo mkdir -p /etc/homewizard_exporter
sudo install -m0644 config.example.yaml /etc/homewizard_exporter/config.yaml   # then edit devices

sudo systemctl daemon-reload
sudo systemctl enable --now homewizard_exporter
```

The unit runs under `DynamicUser=yes` in a hardened sandbox. Device tokens live
in `/var/lib/homewizard_exporter`, created and owned automatically.

## Quick start

```bash
# 1. find your devices
homewizard_exporter discover

# 2. write down what it found
cp config.example.yaml config.yaml    # then edit devices:
homewizard_exporter -config config.yaml -check

# 3. run it
homewizard_exporter -config config.yaml    # scrape http://localhost:9833/metrics
```

Or skip the file entirely for a first look:

```bash
HOMEWIZARD_DEVICES=192.168.1.10,192.168.1.11 homewizard_exporter
```

Every value in [`config.example.yaml`](config.example.yaml) is the default, and
the only required setting is a device's `host`.

## Devices and API versions

HomeWizard has two local APIs, and which one a device speaks depends on the
product and its firmware:

| Product | Type | v1 (HTTP) | v2 (HTTPS + token) |
| --- | --- | --- | --- |
| P1 Meter | `HWE-P1` | yes | yes |
| kWh Meter, 1 and 3 phase | `HWE-KWH1`, `HWE-KWH3`, `SDM230-wifi`, `SDM630-wifi` | yes | yes |
| Energy Socket | `HWE-SKT` | yes | planned |
| Watermeter (USB-powered only) | `HWE-WTR` | yes | planned |
| Plug-In Battery | `HWE-BAT` | no | yes |

**You do not have to work out which.** With `api_version: auto` (the default)
the exporter probes the device and picks:

- If a token is configured, it tries v2 first, always available, authenticated,
  and the only way to read a Plug-In Battery.
- Otherwise it tries v1, which needs no credentials but must be switched on in
  the HomeWizard app under *Settings → Meters → your meter → Local API*.

The product type, serial, firmware and available endpoints all come from the
device's own `/api` response, so a config entry can be one line.

## Authentication

The v1 API needs nothing. The v2 API needs a bearer token, and getting one
requires physically pressing the button on the device; possession of the
hardware *is* the credential.

```bash
homewizard_exporter pair 192.168.1.10 -o /var/lib/homewizard_exporter/p1.token
```

It will ask you to press the button and keep retrying until you do (on a kWh
Meter, hold the Wi-Fi pair button for 1–3 seconds). Point `token_file` at the
result, or set `HOMEWIZARD_TOKEN_<DEVICE>`, the device name upper-cased with
anything unusual replaced by an underscore, so a device named `kwh meter` reads
`HOMEWIZARD_TOKEN_KWH_METER`.

Tokens grant full access to a device's data. Keep them out of the config file
and out of your shell history; `pair -o` writes them `0600`.

### Pairing in a container

Pair from inside the container that is already running, so the token lands on
the volume the exporter reads:

```bash
docker exec -it homewizard_exporter \
  homewizard_exporter pair 192.168.1.10 -o /var/lib/homewizard_exporter/p1.token

# Compose
docker compose exec homewizard_exporter \
  homewizard_exporter pair 192.168.1.10 -o /var/lib/homewizard_exporter/p1.token
```

The name appears twice on purpose. `docker exec` does not go through the
image's `ENTRYPOINT`, so the binary has to be named as well as the container.
`-it` keeps the "press the button" prompt visible while it waits, and lets
Ctrl-C stop the wait rather than leaving you at the meter wondering.

`-o` is not a convenience here. The image is distroless and has no shell, so
there is nothing to redirect with: `docker exec … sh -c 'pair … > token'`
fails with *executable file not found*. Writing the file is the command's job.

If the exporter is not running yet, a throwaway container pairs just as well,
and this one *does* use the entrypoint:

```bash
docker run --rm -it -v homewizard-tokens:/var/lib/homewizard_exporter \
  ghcr.io/marcelblijleven/homewizard_exporter:latest \
  pair 192.168.1.10 -o /var/lib/homewizard_exporter/p1.token
```

Tokens are read at startup, so point the device at the file and restart:

```yaml
devices:
  - name: p1
    host: 192.168.1.10
    token_file: /var/lib/homewizard_exporter/p1.token
```

```bash
docker restart homewizard_exporter
```

Two things that bite in a container rather than on a host: use the device's IP,
because mDNS names do not resolve inside one; and `discover` needs `--network
host` on Linux to receive the multicast it listens for; on macOS or Windows,
run it from a checkout on your workstation instead.

### Certificate verification

Devices serve the v2 API over HTTPS with a certificate signed by HomeWizard's
own CA, which is embedded in the binary. `tls.mode: verify` (the default)
chains the certificate to that CA **and** checks the name it carries.

That name is a Common Name of the form `appliance/<type>/<serial>` rather than a
Subject Alternative Name, so Go's built-in hostname check cannot be used: it
stopped honouring Common Names in 1.15. The exporter does the equivalent by
hand instead. A pleasant side effect: the TLS handshake alone identifies the
device, before any credential is presented, which is how `pair` can tell you
which box to walk over to.

`tls.mode: insecure` skips all of it, for a stand-in device.

## Metrics

Every series is labelled `device` with the name from your config (defaulting to
the host). Attributes that describe rather than measure live on info series, so
the data series stay cheap.

Readings, exported only when the device actually reports them:

| Metric | Notes |
| --- | --- |
| `homewizard_active_power_watts` | negative when exporting to the grid |
| `homewizard_active_power_phase_watts{phase}` | `l1`, `l2`, `l3` |
| `homewizard_active_voltage_volts` / `_phase_volts{phase}` | |
| `homewizard_active_current_amperes` / `_phase_amperes{phase}` | the total is a sum of absolutes, not a net |
| `homewizard_active_apparent_power_voltamperes` | plus a `_phase_` variant |
| `homewizard_active_reactive_power_voltamperes_reactive` | plus a `_phase_` variant |
| `homewizard_active_apparent_current_amperes` | plus a `_phase_` variant |
| `homewizard_active_reactive_current_amperes` | plus a `_phase_` variant |
| `homewizard_active_power_factor_ratio` | plus a `_phase_` variant |
| `homewizard_frequency_hertz` | |
| `homewizard_energy_import_kwh_total` | counter |
| `homewizard_energy_export_kwh_total` | counter |
| `homewizard_energy_{import,export}_tariff_kwh_total{tariff}` | `1`–`4` |
| `homewizard_active_tariff` | which one is in effect |

P1 Meter extras: `homewizard_smart_meter_info{meter_model,protocol_version,unique_id}`,
`homewizard_voltage_{sag,swell}_count_total{phase}`,
`homewizard_{power,long_power}_fail_count_total`,
`homewizard_measurement_timestamp_seconds`, and, for meters billed on a
capacity rate, `homewizard_average_power_15m_watts`,
`homewizard_monthly_power_peak_watts` and its `_timestamp_seconds`.

Gas, water and heat meters behind the smart meter:
`homewizard_external_meter_value{unique_id,type,unit}` and
`homewizard_external_meter_timestamp_seconds`. The unit is a label because it
genuinely differs: `m3` for gas and water, `GJ` for heat.

Energy Socket: `homewizard_socket_power_on`, `_switch_lock`,
`_brightness_ratio`.

Watermeter: `homewizard_active_water_liters_per_minute`,
`homewizard_water_m3_total`.

Plug-In Battery: `homewizard_battery_state_of_charge_ratio`,
`homewizard_battery_cycles_total`. The meter that controls a battery group also
reports `homewizard_batteries_{power,target_power,max_consumption,max_production}_watts`,
`homewizard_batteries_count`, `homewizard_batteries_charge_to_full`,
`homewizard_batteries_mode_info{mode}` and
`homewizard_batteries_permission{permission}`.

Device health: `homewizard_device_info{product_type,product_name,serial,firmware_version,api,api_version}`,
`homewizard_wifi_info{ssid}`, `homewizard_wifi_rssi_dbm` (v2) or
`homewizard_wifi_strength_ratio` (v1), `homewizard_uptime_seconds`,
`homewizard_cloud_enabled`, `homewizard_status_led_brightness_ratio`,
`homewizard_api_v1_enabled`.

Exporter health: `homewizard_up`, `homewizard_devices_total`,
`homewizard_last_update_timestamp_seconds`, `homewizard_build_info` and
`homewizard_poll_{total,errors_total,duration_seconds,last_success_timestamp_seconds}`.

### Extra notes

#### An absent series is not a zero

Every field of the API is optional, and devices send only what they have. A
1-phase meter sends no `l2`, a meter without a gas connection sends no gas, a
socket has no thermometer. The exporter exports **nothing** in those cases
rather than a zero, so `homewizard_active_power_phase_watts` having three series
means three phases, not three wires two of which are asleep.

The practical consequence: write alerts against `absent()` rather than `== 0`
where you mean "this device does not report that".

#### Energy in kWh, not joules

Prometheus convention prefers base units. Every meter faceplate, tariff and
energy bill speaks kWh, and so does the API. Converting would make every
dashboard and every alert threshold a puzzle for no gain.

#### Legacy duplicate fields

The API repeats some values for backwards compatibility, and the exporter
passes them through faithfully rather than second-guessing the device:

- On an Energy Socket and a 1-phase kWh Meter, `active_power_l1_w` is
  documented as identical to `active_power_w`, so `_phase_watts{phase="l1"}`
  will match the total.
- On those same devices `total_power_import_t1_kwh` equals
  `total_power_import_kwh`, so the `tariff="1"` counter matches the total.

Summing per-phase or per-tariff series still gives the right answer; they are
separate metric *names* from the totals precisely so that a `sum()` cannot pick
up both.

#### The v1 API can be switched off

It is off until you enable it in the app, and v2 can turn it off again. A device
in that state answers `403` with code `202`; the exporter says so in as many
words rather than passing on a bare 403.

#### Watermeter on batteries

A battery-powered Watermeter joins Wi-Fi only a few times a day, so its API is
effectively unavailable. Power it over USB if you want to monitor it.

## Polling

Scrapes never touch a device. Each device gets its own goroutine polling on its
own schedule, publishing into a shared snapshot; `/metrics` reads whatever was
published last. Prometheus can scrape as often as it likes.

HomeWizard asks for no more than one request every 500 ms, which is the floor
the config accepts. The default is 10 s, which is comfortably more data than a
15 s scrape interval can use.

A failed poll keeps the previous readings and leaves the timestamps alone, so
staleness shows up in `homewizard_up` and
`homewizard_poll_last_success_timestamp_seconds` rather than data vanishing
mid-graph.

Devices are connected to lazily and reconnected automatically. A meter rebooting
for a firmware update, or a socket switched off at the wall, comes back on its
own without restarting the exporter.

`/healthz` fails only when *no* device has fresh readings; one unplugged socket
should not take the process out of service behind a load balancer. Per-device
health is what `homewizard_up` is for.

## Dashboard

Set `dashboard.enabled: true`, or `HOMEWIZARD_DASHBOARD=true`, and the exporter
serves a card per device: what it is, what it is doing right now, and its
meter readings. Import and export are coloured differently, because the sign of
a number is easy to miss and it is the whole story.

The household total across the top counts only P1 Meters. Adding an Energy
Socket would count the same kettle twice: once where the electricity enters the
house and once where it is used.

## Development

```bash
make check      # vet, gofmt, golangci-lint, go test -race
make fake       # run against the fixtures, no hardware needed
make discover   # find real devices on your network
make capture HOST=192.168.1.10    # refresh the fixtures from a real device
```

`make fake` starts `cmd/fakedevice`, which replays the fixtures as five devices
across both API versions, including HTTPS with a certificate whose Common Name
follows the real scheme, so the certificate verification path is exercised
rather than skipped. The dashboard comes up on <http://localhost:9833/>.

`homewizard_exporter capture` saves real device responses to
`internal/homewizard/testdata/`, scrubbing serials, meter identifiers and Wi-Fi
network names so they can be committed. The parser tests run against those
files, so they describe devices that exist rather than ones imagined from the
documentation.

## License

MIT
