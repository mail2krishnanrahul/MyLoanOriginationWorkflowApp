export function hashStringToTailwindColor(str: string): string {
    let hash = 0;
    for (let i = 0; i < str.length; i++) {
        hash = str.charCodeAt(i) + ((hash << 5) - hash);
    }

    // Pick from 8 distinct tailwind colors (excluding gray/white)
    // 50 background and 700 text combinations
    const colors = [
        'bg-red-50 text-red-700',
        'bg-orange-50 text-orange-700',
        'bg-amber-50 text-amber-700',
        'bg-green-50 text-green-700',
        'bg-emerald-50 text-emerald-700',
        'bg-blue-50 text-blue-700',
        'bg-indigo-50 text-indigo-700',
        'bg-purple-50 text-purple-700',
        'bg-pink-50 text-pink-700',
    ];

    const index = Math.abs(hash) % colors.length;
    return colors[index];
}
