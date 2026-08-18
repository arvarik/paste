const typography = require('@tailwindcss/typography');

module.exports = {
    content: [
        './templates/**/*.html',
        './static/js/**/*.js'
    ],
    darkMode: 'class',
    theme: {
        extend: {
            colors: {
                dark: {
                    900: '#0f1115',
                    800: '#16191f',
                    700: '#22262f',
                    600: '#323844'
                },
                primary: {
                    400: '#60a5fa',
                    500: '#3b82f6',
                    600: '#2563eb'
                }
            },
            animation: {
                'fade-in': 'fadeIn 0.2s ease-out',
                'slide-in': 'slideIn 0.2s ease-out'
            },
            keyframes: {
                fadeIn: {
                    '0%': { opacity: '0' },
                    '100%': { opacity: '1' }
                },
                slideIn: {
                    '0%': { opacity: '0', transform: 'translateX(-10px)' },
                    '100%': { opacity: '1', transform: 'translateX(0)' }
                }
            }
        }
    },
    plugins: [typography]
};
