/**
 * Date formatting utilities
 */

/**
 * Format a date as relative time (e.g., "5 min ago", "2 hours ago")
 */
export function formatRelativeTime(date: string | Date): string {
	const now = new Date();
	const dateObj = typeof date === 'string' ? new Date(date) : date;
	const diffMs = now.getTime() - dateObj.getTime();
	const diffMins = Math.floor(diffMs / 60000);
	const diffHours = Math.floor(diffMins / 60);
	const diffDays = Math.floor(diffHours / 24);

	if (diffMins < 1) return 'Just now';
	if (diffMins < 60) return `${diffMins} min ago`;
	if (diffHours < 24) return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;
	return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`;
}
