import axios, { AxiosError, AxiosInstance, InternalAxiosRequestConfig } from 'axios';

// Create base Axios instance
export const api: AxiosInstance = axios.create({
    baseURL: import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1',
    headers: {
        'Content-Type': 'application/json',
    },
    timeout: 10000,
    withCredentials: true, // For passing cookies if needed
});

// Request Interceptor: Add Authorization header
api.interceptors.request.use(
    (config: InternalAxiosRequestConfig) => {
        // In a real app, you'd fetch this from Zustand/Redux or localStorage
        const token = localStorage.getItem('auth_token');
        if (token && config.headers) {
            config.headers.Authorization = `Bearer ${token}`;
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
