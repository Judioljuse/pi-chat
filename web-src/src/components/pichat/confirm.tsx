import { useCallback, useState, type ReactNode } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

export interface ConfirmOptions {
  title: ReactNode
  description?: ReactNode
  confirmText?: string
  destructive?: boolean
}

type Pending = ConfirmOptions & { resolve: (ok: boolean) => void }

/** Promise-based alert-dialog: `if (await ask({...})) doIt()`. Render `dialog` once in the component. */
export function useConfirm() {
  const [pending, setPending] = useState<Pending | null>(null)
  const [open, setOpen] = useState(false)

  const ask = useCallback(
    (o: ConfirmOptions) =>
      new Promise<boolean>((resolve) => {
        setPending({ ...o, resolve })
        setOpen(true)
      }),
    [],
  )
  const settle = (ok: boolean) => {
    pending?.resolve(ok)
    setOpen(false)
  }

  const dialog = (
    <AlertDialog open={open} onOpenChange={(o) => !o && settle(false)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
          {pending?.description && (
            <AlertDialogDescription className="whitespace-pre-line">{pending.description}</AlertDialogDescription>
          )}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            variant={pending?.destructive ? 'destructive' : 'default'}
            onClick={() => settle(true)}
          >
            {pending?.confirmText ?? '确定'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
  return { ask, dialog }
}
