import type { Config } from 'tailwindcss';
import forms from '@tailwindcss/forms';

const config: Config = {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Geist', 'Inter', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Fira Code', 'ui-monospace', 'SFMono-Regular', 'monospace']
      },
      colors: {
        brand: {
          50: '#eef7ff',
          100: '#d7ebff',
          200: '#afd8ff',
          300: '#75bdff',
          400: '#389fff',
          500: '#0f82ff',
          600: '#0063d6',
          700: '#004daa',
          800: '#04438d',
          900: '#0a3a74',
          950: '#07254a'
        },
        accent: {
          50: '#fff9ed',
          100: '#fff0cf',
          200: '#ffdc97',
          300: '#ffc65e',
          400: '#ffad2f',
          500: '#fb8f06',
          600: '#dd6802',
          700: '#b74906',
          800: '#94380c',
          900: '#7a300d',
          950: '#461703'
        },
        neutral: {
          25: '#fcfdff',
          50: '#f8fafc',
          100: '#f1f5f9',
          200: '#e2e8f0',
          300: '#cbd5e1',
          400: '#94a3b8',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#1e293b',
          900: '#0f172a',
          950: '#020617'
        },
        success: {
          50: '#ecfdf3',
          500: '#16a34a',
          700: '#15803d'
        },
        warning: {
          50: '#fffbeb',
          500: '#d97706',
          700: '#b45309'
        },
        danger: {
          50: '#fef2f2',
          500: '#dc2626',
          700: '#b91c1c'
        },
        info: {
          50: '#eff6ff',
          500: '#2563eb',
          700: '#1d4ed8'
        }
      },
      boxShadow: {
        panel: '0 2px 8px rgb(15 23 42 / 0.08), 0 1px 2px rgb(15 23 42 / 0.08)',
        hover: '0 8px 24px rgb(15 23 42 / 0.12)',
        focus: '0 0 0 3px rgb(15 130 255 / 0.25)'
      },
      animation: {
        shimmer: 'shimmer 1.6s linear infinite',
        'fade-in': 'fadeIn 220ms ease-out',
        'slide-up': 'slideUp 240ms ease-out'
      },
      keyframes: {
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { transform: 'translateY(6px)', opacity: '0' },
          '100%': { transform: 'translateY(0)', opacity: '1' }
        }
      }
    }
  },
  plugins: [forms]
};

export default config;
