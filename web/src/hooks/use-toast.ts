import { toast as sonnerToast } from 'sonner';

interface ToastProps {
  title?: string;
  description?: string;
  variant?: 'default' | 'destructive';
}

// 定义在模块级别，每次渲染拿到的都是同一个函数，可以放进 useEffect / useCallback 的依赖里
const toast = ({ title, description, variant }: ToastProps) => {
  if (variant === 'destructive') {
    sonnerToast.error(title || 'Error', {
      description,
    });
  } else {
    sonnerToast.success(title || 'Success', {
      description,
    });
  }
};

const result = { toast };

export function useToast() {
  return result;
}
