'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  Tv,
  Link as LinkIcon,
  FileText,
  RefreshCw,
  LogOut,
  Settings,
  ScrollText,
  Database,
} from 'lucide-react';
import { Button } from '@/components/ui/button';

import { useAuthStore } from '@/store/authStore';

const navigation = [
  { name: '频道管理', href: '/channels', icon: Tv },
  { name: '频道映射', href: '/mappings', icon: LinkIcon },
  { name: '数据源', href: '/custom-sources', icon: Database },
  { name: '节目管理', href: '/programs', icon: FileText },
  { name: 'EPG同步', href: '/sync', icon: RefreshCw },
  { name: '系统日志', href: '/logs', icon: ScrollText },
  { name: '账户设置', href: '/settings', icon: Settings },
];

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const { user, logout, isHydrated } = useAuthStore();

  useEffect(() => {
    if (isHydrated && !user) {
      router.push('/login/');
    }
  }, [user, router, isHydrated]);

  const handleLogout = () => {
    logout();
    router.push('/login/');
  };

  if (!isHydrated) {
    return (
      <div className="min-h-screen bg-gray-50 flex items-center justify-center">
        <div className="text-center">
          <div className="inline-block animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
          <p className="mt-4 text-gray-500">加载中...</p>
        </div>
      </div>
    );
  }

  if (!user) {
    return null;
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="fixed inset-y-0 left-0 z-50 w-64 bg-white shadow-lg">
        <div className="flex h-full flex-col">
          <div className="flex h-16 items-center justify-center border-b px-6">
            <h1 className="text-xl font-bold text-gray-900">EPG管理后台</h1>
          </div>

          <nav className="flex-1 space-y-1 px-3 py-4">
            {navigation.map((item) => {
              const isActive = pathname === `${item.href}/`;
              return (
                <Link
                  key={item.name}
                  href={item.href}
                  className={`group flex items-center rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                    isActive
                      ? 'bg-blue-50 text-blue-600'
                      : 'text-gray-700 hover:bg-gray-100 hover:text-gray-900'
                  }`}
                >
                  <item.icon
                    className={`mr-3 h-5 w-5 shrink-0 ${
                      isActive ? 'text-blue-600' : 'text-gray-400 group-hover:text-gray-500'
                    }`}
                  />
                  {item.name}
                </Link>
              );
            })}
          </nav>

        
          <div className="border-t p-4">
            <div className="mb-3 flex items-center">
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-gray-900 truncate">
                  {user.username}
                </p>
                <p className="text-xs text-gray-500 truncate">{user.role}</p>
              </div>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={handleLogout}
              className="w-full"
            >
              <LogOut className="mr-2 h-4 w-4" />
              退出登录
            </Button>
          </div>
        </div>
      </div>

  
      <div className="pl-64">
        <main className="py-8 px-8">
          <div className="mx-auto max-w-7xl">{children}</div>
        </main>
      </div>

      
    </div>
  );
}
