import { format, formatDistanceStrict, isToday, parseISO } from 'date-fns';

export function formatDateTime(value?: string) {
  if (!value) {
    return '--';
  }

  const parsed = parseISO(value);
  if (Number.isNaN(parsed.getTime())) {
    return '--';
  }

  if (isToday(parsed)) {
    return `Today, ${format(parsed, 'HH:mm')}`;
  }

  return format(parsed, 'dd MMM yyyy, HH:mm');
}

export function formatDate(value?: string) {
  if (!value) {
    return '--';
  }

  const parsed = parseISO(value);
  if (Number.isNaN(parsed.getTime())) {
    return '--';
  }

  return format(parsed, 'dd MMM yyyy');
}

export function fromNow(value?: string) {
  if (!value) {
    return '--';
  }

  const parsed = parseISO(value);
  if (Number.isNaN(parsed.getTime())) {
    return '--';
  }

  return formatDistanceStrict(parsed, new Date(), { addSuffix: true });
}

export function formatMinutesRemaining(minutes: number) {
  if (minutes < 0) {
    return `${Math.abs(minutes)}m overdue`;
  }

  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  if (hours === 0) {
    return `${mins}m remaining`;
  }

  return `${hours}h ${mins}m remaining`;
}
