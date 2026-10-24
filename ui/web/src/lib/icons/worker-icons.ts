/**
 * Worker Agent Icons - Extracted from Lucide (MIT/ISC)
 * @see https://lucide.dev/icons/
 */

export type IconNode = [string, Record<string, string>][]
export type IconName = keyof typeof WORKER_ICONS

export const WORKER_ICONS = {
	// Communication
	'message-square': [
		[
			'path',
			{
				d: 'M22 17a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586l-2.202 2.202A.71.71 0 0 1 2 21.286V5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2z'
			}
		]
	],
	'message-circle': [
		[
			'path',
			{
				d: 'M2.992 16.342a2 2 0 0 1 .094 1.167l-1.065 3.29a1 1 0 0 0 1.236 1.168l3.413-.998a2 2 0 0 1 1.099.092 10 10 0 1 0-4.777-4.719'
			}
		]
	],
	mail: [
		['path', { d: 'm22 7-8.991 5.727a2 2 0 0 1-2.009 0L2 7' }],
		['rect', { x: '2', y: '4', width: '20', height: '16', rx: '2' }]
	],
	phone: [
		[
			'path',
			{
				d: 'M13.832 16.568a1 1 0 0 0 1.213-.303l.355-.465A2 2 0 0 1 17 15h3a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2A18 18 0 0 1 2 4a2 2 0 0 1 2-2h3a2 2 0 0 1 2 2v3a2 2 0 0 1-.8 1.6l-.468.351a1 1 0 0 0-.292 1.233 14 14 0 0 0 6.392 6.384'
			}
		]
	],
	headset: [
		[
			'path',
			{
				d: 'M3 11h3a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-5Zm0 0a9 9 0 1 1 18 0m0 0v5a2 2 0 0 1-2 2h-1a2 2 0 0 1-2-2v-3a2 2 0 0 1 2-2h3Z'
			}
		],
		['path', { d: 'M21 16v2a4 4 0 0 1-4 4h-5' }]
	],

	// AI/Automation
	code: [
		['polyline', { points: '16 18 22 12 16 6' }],
		['polyline', { points: '8 6 2 12 8 18' }]
	],
	bot: [
		['path', { d: 'M12 8V4H8' }],
		['rect', { width: '16', height: '12', x: '4', y: '8', rx: '2' }],
		['path', { d: 'M2 14h2' }],
		['path', { d: 'M20 14h2' }],
		['path', { d: 'M15 13v2' }],
		['path', { d: 'M9 13v2' }]
	],
	brain: [
		['path', { d: 'M12 18V5' }],
		['path', { d: 'M15 13a4.17 4.17 0 0 1-3-4 4.17 4.17 0 0 1-3 4' }],
		['path', { d: 'M17.598 6.5A3 3 0 1 0 12 5a3 3 0 1 0-5.598 1.5' }],
		['path', { d: 'M17.997 5.125a4 4 0 0 1 2.526 5.77' }],
		['path', { d: 'M18 18a4 4 0 0 0 2-7.464' }],
		['path', { d: 'M19.967 17.483A4 4 0 1 1 12 18a4 4 0 1 1-7.967-.517' }],
		['path', { d: 'M6 18a4 4 0 0 1-2-7.464' }],
		['path', { d: 'M6.003 5.125a4 4 0 0 0-2.526 5.77' }]
	],
	sparkles: [
		[
			'path',
			{
				d: 'M11.017 2.814a1 1 0 0 1 1.966 0l1.051 5.558a2 2 0 0 0 1.594 1.594l5.558 1.051a1 1 0 0 1 0 1.966l-5.558 1.051a2 2 0 0 0-1.594 1.594l-1.051 5.558a1 1 0 0 1-1.966 0l-1.051-5.558a2 2 0 0 0-1.594-1.594l-5.558-1.051a1 1 0 0 1 0-1.966l5.558-1.051a2 2 0 0 0 1.594-1.594z'
			}
		],
		['path', { d: 'M20 2v4' }],
		['path', { d: 'M22 4h-4' }],
		['circle', { cx: '4', cy: '20', r: '2' }]
	],
	zap: [
		[
			'path',
			{
				d: 'M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z'
			}
		]
	],
	cpu: [
		['path', { d: 'M12 20v2' }],
		['path', { d: 'M12 2v2' }],
		['path', { d: 'M17 20v2' }],
		['path', { d: 'M17 2v2' }],
		['path', { d: 'M2 12h2' }],
		['path', { d: 'M2 17h2' }],
		['path', { d: 'M2 7h2' }],
		['path', { d: 'M20 12h2' }],
		['path', { d: 'M20 17h2' }],
		['path', { d: 'M20 7h2' }],
		['path', { d: 'M7 20v2' }],
		['path', { d: 'M7 2v2' }],
		['rect', { x: '4', y: '4', width: '16', height: '16', rx: '2' }],
		['rect', { x: '8', y: '8', width: '8', height: '8', rx: '1' }]
	],
	'circuit-board': [
		['rect', { width: '18', height: '18', x: '3', y: '3', rx: '2' }],
		['path', { d: 'M11 9h4a2 2 0 0 0 2-2V3' }],
		['circle', { cx: '9', cy: '9', r: '2' }],
		['path', { d: 'M7 21v-4a2 2 0 0 1 2-2h4' }],
		['circle', { cx: '15', cy: '15', r: '2' }]
	],

	// Data/Analytics
	'chart-column': [
		['path', { d: 'M3 3v16a2 2 0 0 0 2 2h16' }],
		['path', { d: 'M18 17V9' }],
		['path', { d: 'M13 17V5' }],
		['path', { d: 'M8 17v-3' }]
	],
	'chart-line': [
		['path', { d: 'M3 3v16a2 2 0 0 0 2 2h16' }],
		['path', { d: 'm19 9-5 5-4-4-3 3' }]
	],
	'chart-bar': [
		['path', { d: 'M3 3v16a2 2 0 0 0 2 2h16' }],
		['path', { d: 'M7 16h8' }],
		['path', { d: 'M7 11h12' }],
		['path', { d: 'M7 6h3' }]
	],
	'trending-up': [
		['path', { d: 'M16 7h6v6' }],
		['path', { d: 'm22 7-8.5 8.5-5-5L2 17' }]
	],
	database: [
		['ellipse', { cx: '12', cy: '5', rx: '9', ry: '3' }],
		['path', { d: 'M3 5V19A9 3 0 0 0 21 19V5' }],
		['path', { d: 'M3 12A9 3 0 0 0 21 12' }]
	],

	// Files
	folder: [
		[
			'path',
			{
				d: 'M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z'
			}
		]
	],
	'file-text': [
		[
			'path',
			{
				d: 'M6 22a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.704.706l3.588 3.588A2.4 2.4 0 0 1 20 8v12a2 2 0 0 1-2 2z'
			}
		],
		['path', { d: 'M14 2v5a1 1 0 0 0 1 1h5' }],
		['path', { d: 'M10 9H8' }],
		['path', { d: 'M16 13H8' }],
		['path', { d: 'M16 17H8' }]
	],
	files: [
		[
			'path',
			{ d: 'M15 2h-4a2 2 0 0 0-2 2v11a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V8' }
		],
		[
			'path',
			{
				d: 'M16.706 2.706A2.4 2.4 0 0 0 15 2v5a1 1 0 0 0 1 1h5a2.4 2.4 0 0 0-.706-1.706z'
			}
		],
		['path', { d: 'M5 7a2 2 0 0 0-2 2v11a2 2 0 0 0 2 2h8a2 2 0 0 0 1.732-1' }]
	],
	'folder-open': [
		[
			'path',
			{
				d: 'm6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2'
			}
		]
	],
	pencil: [
		[
			'path',
			{
				d: 'M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z'
			}
		],
		['path', { d: 'm15 5 4 4' }]
	],

	// Infrastructure
	globe: [
		['circle', { cx: '12', cy: '12', r: '10' }],
		['path', { d: 'M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20' }],
		['path', { d: 'M2 12h20' }]
	],
	server: [
		['rect', { width: '20', height: '8', x: '2', y: '2', rx: '2', ry: '2' }],
		['rect', { width: '20', height: '8', x: '2', y: '14', rx: '2', ry: '2' }],
		['line', { x1: '6', x2: '6.01', y1: '6', y2: '6' }],
		['line', { x1: '6', x2: '6.01', y1: '18', y2: '18' }]
	],
	cloud: [
		['path', { d: 'M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z' }]
	],
	webhook: [
		[
			'path',
			{
				d: 'M18 16.98h-5.99c-1.1 0-1.95.94-2.48 1.9A4 4 0 0 1 2 17c.01-.7.2-1.4.57-2'
			}
		],
		[
			'path',
			{ d: 'm6 17 3.13-5.78c.53-.97.1-2.18-.5-3.1a4 4 0 1 1 6.89-4.06' }
		],
		['path', { d: 'm12 6 3.13 5.73C15.66 12.7 16.9 13 18 13a4 4 0 0 1 0 8' }]
	],

	// Security
	shield: [
		[
			'path',
			{
				d: 'M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z'
			}
		]
	],
	'shield-check': [
		[
			'path',
			{
				d: 'M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z'
			}
		],
		['path', { d: 'm9 12 2 2 4-4' }]
	],
	lock: [
		['rect', { width: '18', height: '11', x: '3', y: '11', rx: '2', ry: '2' }],
		['path', { d: 'M7 11V7a5 5 0 0 1 10 0v4' }]
	],
	key: [
		[
			'path',
			{ d: 'm15.5 7.5 2.3 2.3a1 1 0 0 0 1.4 0l2.1-2.1a1 1 0 0 0 0-1.4L19 4' }
		],
		['path', { d: 'm21 2-9.6 9.6' }],
		['circle', { cx: '7.5', cy: '15.5', r: '5.5' }]
	],

	// Users
	user: [
		['path', { d: 'M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2' }],
		['circle', { cx: '12', cy: '7', r: '4' }]
	],
	users: [
		['path', { d: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2' }],
		['path', { d: 'M16 3.128a4 4 0 0 1 0 7.744' }],
		['path', { d: 'M22 21v-2a4 4 0 0 0-3-3.87' }],
		['circle', { cx: '9', cy: '7', r: '4' }]
	],
	'user-plus': [
		['path', { d: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2' }],
		['circle', { cx: '9', cy: '7', r: '4' }],
		['line', { x1: '19', x2: '19', y1: '8', y2: '14' }],
		['line', { x1: '22', x2: '16', y1: '11', y2: '11' }]
	],

	// Settings
	settings: [
		[
			'path',
			{
				d: 'M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915'
			}
		],
		['circle', { cx: '12', cy: '12', r: '3' }]
	],
	'sliders-horizontal': [
		['path', { d: 'M10 5H3' }],
		['path', { d: 'M12 19H3' }],
		['path', { d: 'M14 3v4' }],
		['path', { d: 'M16 17v4' }],
		['path', { d: 'M21 12h-9' }],
		['path', { d: 'M21 19h-5' }],
		['path', { d: 'M21 5h-7' }],
		['path', { d: 'M8 10v4' }],
		['path', { d: 'M8 12H3' }]
	],
	wrench: [
		[
			'path',
			{
				d: 'M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.106-3.105c.32-.322.863-.22.983.218a6 6 0 0 1-8.259 7.057l-7.91 7.91a1 1 0 0 1-2.999-3l7.91-7.91a6 6 0 0 1 7.057-8.259c.438.12.54.662.219.984z'
			}
		]
	],
	hammer: [
		['path', { d: 'm15 12-9.373 9.373a1 1 0 0 1-3.001-3L12 9' }],
		['path', { d: 'm18 15 4-4' }],
		[
			'path',
			{
				d: 'm21.5 11.5-1.914-1.914A2 2 0 0 1 19 8.172v-.344a2 2 0 0 0-.586-1.414l-1.657-1.657A6 6 0 0 0 12.516 3H9l1.243 1.243A6 6 0 0 1 12 8.485V10l2 2h1.172a2 2 0 0 1 1.414.586L18.5 14.5'
			}
		]
	],

	// Status
	bell: [
		['path', { d: 'M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9' }],
		['path', { d: 'M10.3 21a1.94 1.94 0 0 0 3.4 0' }]
	],
	check: [['path', { d: 'M20 6 9 17l-5-5' }]],
	circle: [['circle', { cx: '12', cy: '12', r: '10' }]],
	info: [
		['circle', { cx: '12', cy: '12', r: '10' }],
		['path', { d: 'M12 16v-4' }],
		['path', { d: 'M12 8h.01' }]
	],

	// Media
	image: [
		['rect', { width: '18', height: '18', x: '3', y: '3', rx: '2', ry: '2' }],
		['circle', { cx: '9', cy: '9', r: '2' }],
		['path', { d: 'm21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21' }]
	],
	video: [
		[
			'path',
			{
				d: 'm16 13 5.223 3.482a.5.5 0 0 0 .777-.416V7.87a.5.5 0 0 0-.752-.432L16 10.5'
			}
		],
		['rect', { x: '2', y: '6', width: '14', height: '12', rx: '2' }]
	],
	'audio-lines': [
		['path', { d: 'M2 10v3' }],
		['path', { d: 'M6 6v11' }],
		['path', { d: 'M10 3v18' }],
		['path', { d: 'M14 8v7' }],
		['path', { d: 'M18 5v13' }],
		['path', { d: 'M22 10v3' }]
	],

	// Time
	clock: [
		['circle', { cx: '12', cy: '12', r: '10' }],
		['path', { d: 'M12 6v6l4 2' }]
	],
	calendar: [
		['path', { d: 'M8 2v4' }],
		['path', { d: 'M16 2v4' }],
		['rect', { width: '18', height: '18', x: '3', y: '4', rx: '2' }],
		['path', { d: 'M3 10h18' }]
	],
	timer: [
		['line', { x1: '10', x2: '14', y1: '2', y2: '2' }],
		['line', { x1: '12', x2: '15', y1: '14', y2: '11' }],
		['circle', { cx: '12', cy: '14', r: '8' }]
	],

	// Navigation
	'arrow-right': [
		['path', { d: 'M5 12h14' }],
		['path', { d: 'm12 5 7 7-7 7' }]
	],
	'arrow-up': [
		['path', { d: 'm5 12 7-7 7 7' }],
		['path', { d: 'M12 19V5' }]
	],
	'chevron-right': [['path', { d: 'm9 18 6-6-6-6' }]]
} as const satisfies Record<string, IconNode>

// Icon categories for UI grouping (e.g., icon picker)
export const ICON_CATEGORIES = {
	communication: [
		'message-square',
		'message-circle',
		'mail',
		'phone',
		'headset'
	],
	ai: ['bot', 'brain', 'sparkles', 'zap', 'cpu', 'circuit-board', 'code'],
	data: ['chart-column', 'chart-line', 'chart-bar', 'trending-up', 'database'],
	files: ['file-text', 'files', 'folder-open', 'folder', 'pencil'],
	infrastructure: ['globe', 'server', 'cloud', 'webhook'],
	security: ['shield', 'shield-check', 'lock', 'key'],
	users: ['user', 'users', 'user-plus'],
	settings: ['settings', 'sliders-horizontal', 'wrench', 'hammer'],
	status: ['check', 'circle', 'info', 'bell'],
	media: ['image', 'video', 'audio-lines'],
	time: ['clock', 'calendar', 'timer'],
	navigation: ['arrow-right', 'arrow-up', 'chevron-right']
} as const

// Helper to get all icon names as array
export const ALL_ICON_NAMES = Object.keys(WORKER_ICONS) as IconName[]
