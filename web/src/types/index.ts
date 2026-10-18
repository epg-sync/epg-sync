export interface Channel {
  id: number;
  channel_id: string;
  display_name: string;
  category?: string;
  regexp?: string;
  area?: string;
  logo_url?: string;
  timezone?: string;
  is_active: number;
  created_at: string;
  updated_at: string;
}

export interface ChannelMapping {
  id: number;
  canonical_id: string;
  provider_id: string;
  provider_channel_id: string;
  provider_channel_name?: string;
  confidence: number;
  is_verified: number;
  created_at: string;
  updated_at: string;
}

export interface Program {
  id: number;
  channel_id: string;
  title: string;
  start_time: string;
  end_time: string;
  description?: string;
  provider_id: string;
  original_timezone: string;
  created_at: string;
  updated_at: string;
}

// 程序日志，level 为 zap 的级别值：-1 debug、0 info、1 warn、2 error、更大为 fatal
export interface LogEntry {
  id: number;
  time: string;
  level: number;
  logger: string;
  caller: string;
  message: string;
  fields: Record<string, unknown> | null;
  stack: string;
}

export interface User {
  id: number;
  username: string;
  email?: string;
  role: string;
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  message: string;
  data: {
    token: string;
    user: User;
  };
}

export interface ApiResponse<T = any> {
  data?: T;
  message?: string;
  error?: string;
  code?: string;
}
