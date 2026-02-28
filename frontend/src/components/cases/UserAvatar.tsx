import { hashStringToTailwindColor } from '@/utils/colors';
import type { UserSummary } from '@/types/cases';
import { cn } from '@/lib/cn';

interface UserAvatarProps {
    user: UserSummary;
    size?: 'xs' | 'sm' | 'md';
}

export function UserAvatar({ user, size = 'sm' }: UserAvatarProps) {
    const bgColorClass = hashStringToTailwindColor(user.userId);

    const sizeClasses = {
        xs: 'w-6 h-6 text-xs',
        sm: 'w-8 h-8 text-sm',
        md: 'w-10 h-10 text-base',
    };

    return (
        <div
            className={cn(
                'inline-flex items-center justify-center rounded-full font-medium shrink-0',
                bgColorClass,
                sizeClasses[size]
            )}
            aria-label={user.displayName}
            title={user.displayName}
        >
            {user.initials}
        </div>
    );
}
