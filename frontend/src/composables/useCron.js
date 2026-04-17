import { CronExpressionParser } from 'cron-parser';

const daysOptions = [
  { label: 'Sun', value: 0 }, { label: 'Mon', value: 1 }, { label: 'Tue', value: 2 },
  { label: 'Wed', value: 3 }, { label: 'Thu', value: 4 }, { label: 'Fri', value: 5 },
  { label: 'Sat', value: 6 }
];

export function useCron() {
  function formatCron(cron) {
    if (!cron) return '';
    
    // Detect one-time (has specific date parts and NO wildcards in DOM/Month)
    const parts = cron.split(' ');
    if (parts.length === 5 && parts[2] !== '*' && parts[3] !== '*') {
      return `ONE-TIME`;
    }

    const presets = {
      '0 * * * *': 'Hourly',
      '*/15 * * * *': 'Every 15m',
      '*/30 * * * *': 'Every 30m',
    };
    if (presets[cron]) return presets[cron];

    try {
      // To show the schedule in the user's local time, we parse the UTC cron 
      // and take the next execution's local day/time components.
      let interval;
      try {
        interval = CronExpressionParser.parse(cron, { tz: 'UTC' });
      } catch (e) {
        interval = CronExpressionParser.parse(cron);
      }
      const next = interval.next().toDate();
      const h = String(next.getHours()).padStart(2, '0');
      const min = String(next.getMinutes()).padStart(2, '0');
      const timeStr = `${h}:${min}`;

      const [cMin, cHour, cDom, cMonth, cDow] = parts;
      
      if (cDow !== '*' && cDom === '*' && cMonth === '*') {
        // Weekly - tricky because 'next' only shows the VERY next one.
        // For a simple summary, we can still use the localized day of the week if it's a single day.
        if (!cDow.includes(',') && !cDow.includes('-')) {
          const localDay = daysOptions.find(o => o.value == next.getDay())?.label || '';
          return `Weekly (${localDay}) at ${timeStr}`;
        }
        return `Weekly at ${timeStr}`;
      }
      if (cDom !== '*' && cMonth === '*' && cDow === '*') {
        return `Monthly (Day ${next.getDate()}) at ${timeStr}`;
      }
      if (cDom === '*' && cMonth === '*' && cDow === '*') {
        return `Daily at ${timeStr}`;
      }
    } catch (e) {}

    return cron;
  }

  function getNextRunDateTime(cron) {
    if (!cron) return '';
    try {
      let interval;
      try {
        interval = CronExpressionParser.parse(cron, { tz: 'UTC' });
      } catch (e) {
        interval = CronExpressionParser.parse(cron);
      }
      const next = interval.next().toDate();
      
      // Localized format
      const options = { 
        month: 'short', 
        day: 'numeric', 
        hour: '2-digit', 
        minute: '2-digit',
        hour12: false // Brutalist preference for 24h
      };
      
      return next.toLocaleString(undefined, options);
    } catch (e) {
      console.warn('cron-parser error (getNextRunDateTime):', e.message, 'for cron:', cron);
      return '';
    }
  }

  function getNextRunLabel(cron) {
    if (!cron) return '';
    try {
      let interval;
      try {
        interval = CronExpressionParser.parse(cron, { tz: 'UTC' });
      } catch (e) {
        interval = CronExpressionParser.parse(cron);
      }
      const next = interval.next().toDate();
      return formatRelativeTime(next);
    } catch (e) {
      console.warn('cron-parser error (getNextRunLabel):', e.message, 'for cron:', cron);
      return '';
    }
  }

  function formatRelativeTime(date) {
    const now = new Date();
    const diffMs = date.getTime() - now.getTime();
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHour = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHour / 24);

    if (diffDay > 0) return `In ${diffDay} ${diffDay === 1 ? 'day' : 'days'}`;
    if (diffHour > 0) {
      const remainingMins = diffMin % 60;
      if (remainingMins > 0) return `In ${diffHour}h ${remainingMins}m`;
      return `In ${diffHour} ${diffHour === 1 ? 'hour' : 'hours'}`;
    }
    if (diffMin > 0) return `In ${diffMin} ${diffMin === 1 ? 'min' : 'mins'}`;
    return 'Soon'; 
  }

  return {
    formatCron,
    getNextRunDateTime,
    getNextRunLabel,
    daysOptions
  };
}
