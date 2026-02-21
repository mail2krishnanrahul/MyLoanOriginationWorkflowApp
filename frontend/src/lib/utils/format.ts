export function formatCurrency(amount?: number) {
  if (amount === undefined || Number.isNaN(amount)) {
    return '--';
  }

  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 0
  }).format(amount);
}

export function formatBytes(bytes: number) {
  if (!bytes || bytes <= 0) {
    return '0 B';
  }

  const units = ['B', 'KB', 'MB', 'GB'];
  const power = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const size = bytes / 1024 ** power;
  return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[power]}`;
}
