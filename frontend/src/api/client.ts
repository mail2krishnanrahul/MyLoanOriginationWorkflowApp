import axios, { AxiosError, AxiosInstance, InternalAxiosRequestConfig } from 'axios';

// Create base Axios instance
export const api: AxiosInstance = axios.create({
    baseURL: import.meta.env.VITE_API_URL || '/api',
    headers: {
        'Content-Type': 'application/json',
    },
    timeout: 10000,
    withCredentials: true, // For passing cookies if needed
});

// Request Interceptor: Add Authorization header and Tenant ID
api.interceptors.request.use(
    (config: InternalAxiosRequestConfig) => {
        // In a real app, you'd fetch this from Zustand/Redux or localStorage
        const token = localStorage.getItem('auth_token');
        if (token && config.headers) {
            config.headers.Authorization = `Bearer ${token}`;
        }

        // Inject Tenant ID
        if (config.headers) {
            config.headers['X-Tenant-ID'] = import.meta.env.VITE_TENANT_ID || 'DEFAULT';
        }

        // Inject User ID — required by backend TenantMiddleware to populate UserIDFromContext.
        // Reads from localStorage so a real auth flow can set it; falls back to seeded dev user.
        if (config.headers) {
            const userId = localStorage.getItem('user_id')
                || import.meta.env.VITE_USER_ID
                || '11111111-1111-1111-1111-111111111111';
            config.headers['X-User-ID'] = userId;
        }

        return config;
    },
    (error: AxiosError) => {
        return Promise.reject(error);
    }
);

// Response Interceptor: Handle global errors (e.g. 401 Unauthorized)
api.interceptors.response.use(
    (response) => {
        return response;
    },
    (error: AxiosError) => {
        if (error.response) {
            if (error.response.status === 401) {
                // Redirect to login or handled globally by auth provider
                console.warn('Unauthorized. Redirecting to login...');
                window.dispatchEvent(new CustomEvent('auth:unauthorized'));
            }
        }
        return Promise.reject(error);
    }
);

// Generic Request Helpers
export class ApiClient {
    public static async get<T>(url: string, params?: any): Promise<T> {
        const response = await api.get<T>(url, { params });
        return response.data;
    }

    public static async post<T>(url: string, data?: any): Promise<T> {
        const response = await api.post<T>(url, data);
        return response.data;
    }

    public static async put<T>(url: string, data?: any): Promise<T> {
        const response = await api.put<T>(url, data);
        return response.data;
    }

    public static async patch<T>(url: string, data?: any): Promise<T> {
        const response = await api.patch<T>(url, data);
        return response.data;
    }

    public static async delete<T>(url: string): Promise<T> {
        const response = await api.delete<T>(url);
        return response.data;
    }
}
