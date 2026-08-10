// Dashboard: poll api/snapshot and redraw. No dependencies, no build step.
(function () {
  "use strict";

  var refreshMs = (window.HOMEWIZARD_REFRESH || 10) * 1000;
  var updatedAt = null;

  function watts(w) {
    var abs = Math.abs(w);
    if (abs >= 1000) return (w / 1000).toFixed(2) + " kW";
    return Math.round(w) + " W";
  }

  function energy(kwh) {
    if (kwh >= 1000) return (kwh / 1000).toFixed(2) + " MWh";
    return kwh.toFixed(1) + " kWh";
  }

  function ago(iso) {
    if (!iso) return "never";
    var seconds = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    if (seconds < 90) return Math.round(seconds) + "s ago";
    if (seconds < 5400) return Math.round(seconds / 60) + "m ago";
    return Math.round(seconds / 3600) + "h ago";
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  // Power has a sign and the sign is the story: positive is electricity being
  // bought, negative is electricity being sold. Colour says which without
  // making anyone parse a minus sign.
  function flowClass(w) {
    if (w > 0) return "importing";
    if (w < 0) return "exporting";
    return "";
  }

  function row(parent, key, value) {
    var node = el("div", "row");
    node.appendChild(el("span", "k", key));
    node.appendChild(el("span", "v", value));
    parent.appendChild(node);
  }

  function deviceNode(device) {
    var node = el("div", "device");
    if (!device.up) node.classList.add("offline");

    var head = el("div", "device-head");
    head.appendChild(el("span", "device-name", device.name));
    if (device.api) head.appendChild(el("span", "device-api", device.api));
    node.appendChild(head);
    node.appendChild(el("div", "device-product", device.product));

    // The headline number: grid power for a meter, flow for a water meter,
    // charge for a battery. Whichever the device is actually about.
    if (device.watts !== undefined && device.watts !== null) {
      var reading = el("div", "reading " + flowClass(device.watts), watts(device.watts));
      node.appendChild(reading);
      if (device.watts < 0) {
        node.appendChild(el("div", "reading-note", "exporting to the grid"));
      }
    } else if (device.waterLpm !== undefined && device.waterLpm !== null) {
      node.appendChild(el("div", "reading", device.waterLpm.toFixed(1) + " L/min"));
    } else if (device.up) {
      node.appendChild(el("div", "reading", "-"));
    }

    var rows = el("div", "rows");

    (device.phases || []).forEach(function (phase) {
      row(rows, phase.name, watts(phase.watts));
    });
    if (device.importKwh !== undefined && device.importKwh !== null) {
      row(rows, "imported", energy(device.importKwh));
    }
    if (device.exportKwh !== undefined && device.exportKwh !== null) {
      row(rows, "exported", energy(device.exportKwh));
    }
    if (device.waterM3 !== undefined && device.waterM3 !== null) {
      row(rows, "water", device.waterM3.toFixed(3) + " m³");
    }
    if (device.batteryPct !== undefined && device.batteryPct !== null) {
      row(rows, "charge", Math.round(device.batteryPct) + "%");
    }
    (device.external || []).forEach(function (external) {
      row(rows, external.type.replace(/_/g, " "), external.value + " " + external.unit);
    });
    if (device.socketOn !== undefined && device.socketOn !== null) {
      var state = el("div", "row");
      state.appendChild(el("span", "k", "relay"));
      state.appendChild(el("span", "pill " + (device.socketOn ? "on" : "off"),
        device.socketOn ? "on" : "off"));
      rows.appendChild(state);
    }
    node.appendChild(rows);

    var foot = device.up ? device.host : device.status;
    node.appendChild(el("div", "device-foot", foot));
    node.title = [device.product, device.type, device.serial, device.firmware, device.status]
      .filter(Boolean).join("\n");

    return node;
  }

  function render(view) {
    var status = document.getElementById("status");
    status.dataset.state = view.up ? (view.totals.online < view.totals.devices ? "stale" : "ok") : "down";
    document.getElementById("status-text").textContent = view.status;

    var total = document.getElementById("total-watts");
    total.textContent = watts(view.totals.watts);
    total.className = "value " + flowClass(view.totals.watts);
    document.getElementById("total-watts-label").textContent =
      view.totals.watts < 0 ? "exporting" : "grid";

    document.getElementById("total-water-stat").hidden = !view.totals.hasWater;
    if (view.totals.hasWater) {
      document.getElementById("total-water").textContent =
        view.totals.waterLpm.toFixed(1) + " L/min";
    }

    document.getElementById("total-devices").textContent =
      view.totals.online + " / " + view.totals.devices;

    var devices = view.devices || [];
    var container = document.getElementById("devices");
    container.replaceChildren();
    devices.forEach(function (device) {
      container.appendChild(deviceNode(device));
    });
    document.getElementById("empty").hidden = devices.length > 0;

    updatedAt = view.updatedAt || null;
    paintUpdated();
  }

  // The age of the snapshot is a clock, not a value: it has to be repainted on
  // its own timer, otherwise it only moves when a poll lands and reads as
  // frozen between refreshes -- which is precisely when staleness matters.
  function paintUpdated() {
    if (!updatedAt) return;
    document.getElementById("updated").textContent = "updated " + ago(updatedAt);
  }

  function tick() {
    fetch("api/snapshot", { headers: { Accept: "application/json" } })
      .then(function (response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(render)
      .catch(function (err) {
        var status = document.getElementById("status");
        status.dataset.state = "down";
        document.getElementById("status-text").textContent = "unreachable: " + err.message;
      });
  }

  tick();
  setInterval(tick, refreshMs);
  setInterval(paintUpdated, 1000);
})();
