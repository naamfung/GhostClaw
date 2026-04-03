/**
 * File Upload Service
 *
 * Handles file uploads to the GhostClaw backend server.
 * Supports both remote upload and local file path indication.
 */

export interface UploadResponse {
        success: boolean;
        filename: string;
        size: number;
        path: string;
        url: string;
        message: string;
        error?: string;
}

export interface UploadProgress {
        loaded: number;
        total: number;
        percentage: number;
}

class UploadService {
        /**
         * Upload a file to the server
         *
         * @param file - The file to upload
         * @param onProgress - Optional progress callback
         * @returns Promise resolving to upload response
         */
        async uploadFile(
                file: File,
                onProgress?: (progress: UploadProgress) => void
        ): Promise<UploadResponse> {
                return new Promise((resolve, reject) => {
                        const xhr = new XMLHttpRequest();

                        // Track upload progress
                        xhr.upload.addEventListener('progress', (event) => {
                                if (event.lengthComputable && onProgress) {
                                        onProgress({
                                                loaded: event.loaded,
                                                total: event.total,
                                                percentage: Math.round((event.loaded / event.total) * 100)
                                        });
                                }
                        });

                        xhr.addEventListener('load', () => {
                                if (xhr.status >= 200 && xhr.status < 300) {
                                        try {
                                                const response = JSON.parse(xhr.responseText);
                                                resolve(response);
                                        } catch {
                                                reject(new Error('Failed to parse server response'));
                                        }
                                } else {
                                        try {
                                                const error = JSON.parse(xhr.responseText);
                                                reject(new Error(error.error || 'Upload failed'));
                                        } catch {
                                                reject(new Error(`Upload failed: ${xhr.status}`));
                                        }
                                }
                        });

                        xhr.addEventListener('error', () => {
                                reject(new Error('Network error during upload'));
                        });

                        xhr.addEventListener('abort', () => {
                                reject(new Error('Upload cancelled'));
                        });

                        // Create form data
                        const formData = new FormData();
                        formData.append('file', file);

                        // Send request
                        xhr.open('POST', '/upload');
                        xhr.send(formData);
                });
        }

        /**
         * Upload multiple files
         *
         * @param files - Array of files to upload
         * @param onProgress - Optional progress callback for each file
         * @returns Promise resolving to array of upload responses
         */
        async uploadFiles(
                files: File[],
                onProgress?: (fileIndex: number, progress: UploadProgress) => void
        ): Promise<UploadResponse[]> {
                const results: UploadResponse[] = [];

                for (let i = 0; i < files.length; i++) {
                        const response = await this.uploadFile(files[i], (progress) => {
                                onProgress?.(i, progress);
                        });
                        results.push(response);
                }

                return results;
        }

        /**
         * Format file path message for the model
         *
         * @param path - The file path on the server
         * @param filename - Original filename
         * @returns Formatted message to send to the model
         */
        formatPathMessage(path: string, filename: string): string {
                return `[文件上传成功]\n文件名: ${filename}\n服务器路径: ${path}\n\n请使用 /path ${path} 命令让模型读取此文件。`;
        }

        /**
         * Check if a path looks like a local file path
         *
         * @param path - The path to check
         * @returns True if it looks like a local file path
         */
        isLocalPath(path: string): boolean {
                // Unix absolute path
                if (path.startsWith('/')) return true;
                // Windows absolute path (e.g., C:\, D:\)
                if (/^[A-Za-z]:[/\\]/.test(path)) return true;
                // Home directory
                if (path.startsWith('~/')) return true;
                return false;
        }

        /**
         * Create a local path message for the model
         * This is for users who are on the same machine as the server
         *
         * @param path - The local file path
         * @returns Formatted message to send to the model
         */
        createLocalPathMessage(path: string): string {
                return `/path ${path}`;
        }
}

// Singleton instance
export const uploadService = new UploadService();
