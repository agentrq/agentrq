// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { CronExpressionParser } from "cron-parser";

const daysOptions = [
  { label: "Sun", value: 0 },
  { label: "Mon", value: 1 },
  { label: "Tue", value: 2 },
  { label: "Wed", value: 3 },
  { label: "Thu", value: 4 },
  { label: "Fri", value: 5 },
  { label: "Sat", value: 6 },
];

export function splitSchedule(cron) {
  const trimmed = (cron ?? "").trim();
  const prefixed = /^(?:CRON_TZ|TZ)=(\S+)\s+(.+)$/.exec(trimmed);
  if (prefixed) return { tz: prefixed[1], spec: prefixed[2].trim() };
  return { tz: "UTC", spec: trimmed };
}

export function localZone() {
  try {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (!zone) return "UTC";
    // ICU reports Etc/Unknown when it cannot tell, and neither cron-parser nor
    // the server resolves that. Intl refuses it here, as any zone it cannot use.
    new Intl.DateTimeFormat("en-US", { timeZone: zone });
    return zone;
  } catch (e) {
    return "UTC";
  }
}

/**
 * A cron-parser iterator over `cron`, read in the zone the schedule names.
 * A zone cron-parser does not know throws on the first `next()`, not here, so
 * every caller of this already has to catch.
 */
function iterate(cron) {
  const { tz, spec } = splitSchedule(cron);
  return CronExpressionParser.parse(spec, { tz });
}

const isPlain = (field) => /^\d{1,2}$/.test(field);
const pad = (n) => String(n).padStart(2, "0");

// The sub-hourly presets have no time of day in them, so they name no zone and
// stay byte-identical to the schedules written before zones existed.
const FIXED_PRESETS = {
  "15min": "*/15 * * * *",
  "30min": "*/30 * * * *",
  hourly: "0 * * * *",
  "2hour": "0 */2 * * *",
};

/**
 * The cron for what the schedule picker is showing. Every field is written in
 * `zone`, so no day or hour is converted here and none drifts across DST.
 *
 * @param {{type: string, preset?: string, oneTimeDate?: string, time?: string,
 *          days?: number[], monthDay?: number, zone?: string}} picked
 * @returns {string} the cron, or '' when the picker has nothing to say
 */
export function cronFromSchedule(picked) {
  const zone = picked.zone || localZone();
  const inZone = (spec) => (zone === "UTC" ? spec : `CRON_TZ=${zone} ${spec}`);

  if (picked.type === "none") return "";

  if (picked.type === "onetime") {
    if (!picked.oneTimeDate) return "";
    const d = new Date(picked.oneTimeDate);
    if (Number.isNaN(d.getTime())) return "";
    // The datetime-local input is in the reader's own zone whatever the stored
    // schedule named, so this is the one shape that does not keep its zone.
    const fields = `${d.getMinutes()} ${d.getHours()} ${d.getDate()} ${d.getMonth() + 1} *`;
    return localZone() === "UTC" ? fields : `CRON_TZ=${localZone()} ${fields}`;
  }

  if (FIXED_PRESETS[picked.preset]) return FIXED_PRESETS[picked.preset];

  const [hours, minutes] = (picked.time ?? "").split(":").map(Number);
  if (!Number.isInteger(hours) || !Number.isInteger(minutes)) return "";

  switch (picked.preset) {
    case "12hour": {
      const pair = [hours, (hours + 12) % 24].sort((a, b) => a - b).join(",");
      return inZone(`${minutes} ${pair} * * *`);
    }
    case "daily":
      return inZone(`${minutes} ${hours} * * *`);
    case "weekly":
      // The day it was loaded with; a schedule being set for the first time
      // still carries the whole default week, and takes today.
      return inZone(
        `${minutes} ${hours} * * ${picked.days?.length === 1 ? picked.days[0] : new Date().getDay()}`,
      );
    case "monthly":
      return inZone(
        `${minutes} ${hours} ${Math.min(31, Math.max(1, picked.monthDay ?? 1))} * *`,
      );
    case "custom": {
      const days = [...new Set(picked.days ?? [])]
        .sort((a, b) => a - b)
        .join(",");
      return days ? inZone(`${minutes} ${hours} * * ${days}`) : "";
    }
    default:
      return "";
  }
}

/**
 * What the picker should show for a stored cron, or null when the picker has
 * no way to say it — a list of days, a step, a range or a named month. A null
 * is not a parse failure: the schedule is valid and must be left alone.
 *
 * @returns {{type: string, preset?: string, oneTimeDate?: string, time?: string,
 *            days?: number[], monthDay?: number, zone: string}|null}
 */
export function scheduleFromCron(cron) {
  const { tz, spec } = splitSchedule(cron);
  if (!spec) return null;
  const fields = spec.split(/\s+/);
  if (fields.length !== 5) return null;
  const [min, hour, dom, month, dow] = fields;

  // One-time: a fixed day of a fixed month. Read as an instant so the input,
  // which is always in the reader's own zone, shows the moment it will run.
  if (dom !== "*" && month !== "*") {
    try {
      const at = iterate(cron).next().toDate();
      const date = `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
      const at_ = `${date}T${pad(at.getHours())}:${pad(at.getMinutes())}`;
      return { type: "onetime", oneTimeDate: at_, zone: localZone() };
    } catch (e) {
      return null;
    }
  }

  const fixed = Object.keys(FIXED_PRESETS).find(
    (p) => FIXED_PRESETS[p] === spec,
  );
  if (fixed) return { type: "repeated", preset: fixed, zone: tz };

  if (!isPlain(min)) return null;
  const time = (h) => `${pad(h)}:${pad(Number(min))}`;

  if (dom === "*" && month === "*" && dow === "*") {
    const hours = hour.split(",");
    if (
      hours.length === 2 &&
      hours.every(isPlain) &&
      Math.abs(hours[1] - hours[0]) === 12
    ) {
      return {
        type: "repeated",
        preset: "12hour",
        time: time(Math.min(...hours.map(Number))),
        zone: tz,
      };
    }
    if (!isPlain(hour)) return null;
    return {
      type: "repeated",
      preset: "daily",
      time: time(Number(hour)),
      zone: tz,
    };
  }

  if (!isPlain(hour)) return null;

  if (dow !== "*" && dom === "*" && month === "*") {
    const days = dow.split(",");
    if (!days.every(isPlain)) return null;
    const values = days.map(Number);
    if (values.some((d) => d > 6)) return null;
    const preset = values.length === 1 && values[0] === 0 ? "weekly" : "custom";
    return {
      type: "repeated",
      preset,
      time: time(Number(hour)),
      days: values,
      zone: tz,
    };
  }

  if (dom !== "*" && month === "*" && dow === "*") {
    if (!isPlain(dom) || Number(dom) < 1 || Number(dom) > 31) return null;
    return {
      type: "repeated",
      preset: "monthly",
      time: time(Number(hour)),
      monthDay: Number(dom),
      zone: tz,
    };
  }

  return null;
}

export function useCron() {
  function formatCron(cron) {
    if (!cron) return "";

    const { spec } = splitSchedule(cron);
    const parts = spec.split(/\s+/);

    // Detect one-time (has specific date parts and NO wildcards in DOM/Month)
    if (parts.length === 5 && parts[2] !== "*" && parts[3] !== "*") {
      return `ONE-TIME`;
    }

    const presets = {
      "0 * * * *": "Hourly",
      "*/15 * * * *": "Every 15m",
      "*/30 * * * *": "Every 30m",
    };
    if (presets[spec]) return presets[spec];

    try {
      // To show the schedule in the user's local time, we parse it in the zone
      // it names and take the next execution's local day/time components.
      const next = iterate(cron).next().toDate();
      const h = String(next.getHours()).padStart(2, "0");
      const min = String(next.getMinutes()).padStart(2, "0");
      const timeStr = `${h}:${min}`;

      const [cMin, cHour, cDom, cMonth, cDow] = parts;

      if (cDow !== "*" && cDom === "*" && cMonth === "*") {
        // Weekly - tricky because 'next' only shows the VERY next one.
        // For a simple summary, we can still use the localized day of the week if it's a single day.
        if (!cDow.includes(",") && !cDow.includes("-")) {
          // daysOptions runs Sun..Sat, so getDay() indexes it directly.
          const localDay = daysOptions[next.getDay()].label;
          return `Weekly (${localDay}) at ${timeStr}`;
        }
        return `Weekly at ${timeStr}`;
      }
      if (cDom !== "*" && cMonth === "*" && cDow === "*") {
        return `Monthly (Day ${next.getDate()}) at ${timeStr}`;
      }
      if (cDom === "*" && cMonth === "*" && cDow === "*") {
        return `Daily at ${timeStr}`;
      }
    } catch (e) {}

    return cron;
  }

  function getNextRunDateTime(cron) {
    if (!cron) return "";
    try {
      const next = iterate(cron).next().toDate();

      // Localized format
      const options = {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false, // Brutalist preference for 24h
      };

      return next.toLocaleString(undefined, options);
    } catch (e) {
      console.warn(
        "cron-parser error (getNextRunDateTime):",
        e.message,
        "for cron:",
        cron,
      );
      return "";
    }
  }

  function getNextRunLabel(cron) {
    if (!cron) return "";
    try {
      return formatRelativeTime(iterate(cron).next().toDate());
    } catch (e) {
      console.warn(
        "cron-parser error (getNextRunLabel):",
        e.message,
        "for cron:",
        cron,
      );
      return "";
    }
  }

  function formatRelativeTime(date) {
    const now = new Date();
    const diffMs = date.getTime() - now.getTime();
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHour = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHour / 24);

    if (diffDay > 0) return `In ${diffDay} ${diffDay === 1 ? "day" : "days"}`;
    if (diffHour > 0) {
      const remainingMins = diffMin % 60;
      if (remainingMins > 0) return `In ${diffHour}h ${remainingMins}m`;
      return `In ${diffHour} ${diffHour === 1 ? "hour" : "hours"}`;
    }
    if (diffMin > 0) return `In ${diffMin} ${diffMin === 1 ? "min" : "mins"}`;
    return "Soon";
  }

  function getNextRunDate(cron) {
    if (!cron) return new Date(8640000000000000); // Far future
    try {
      return iterate(cron).next().toDate();
    } catch (e) {
      return new Date(8640000000000000); // Far future
    }
  }

  return {
    formatCron,
    getNextRunDate,
    getNextRunDateTime,
    getNextRunLabel,
    cronFromSchedule,
    scheduleFromCron,
    splitSchedule,
    localZone,
    daysOptions,
  };
}
