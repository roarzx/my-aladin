import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type AnchorHTMLAttributes,
  type ReactNode,
} from 'react'

interface RouterValue {
  pathname: string
  search: string
  query: URLSearchParams
  navigate: (to: string, options?: { replace?: boolean }) => void
}

const RouterContext = createContext<RouterValue | null>(null)

function readLocation(): { pathname: string; search: string } {
  return { pathname: window.location.pathname, search: window.location.search }
}

export function RouterProvider({ children }: { children: ReactNode }) {
  const [location, setLocation] = useState(readLocation)

  useEffect(() => {
    const onPopState = () => setLocation(readLocation())
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const navigate = useCallback((to: string, options?: { replace?: boolean }) => {
    const url = new URL(to, window.location.origin)
    const next = `${url.pathname}${url.search}${url.hash}`
    const current = `${window.location.pathname}${window.location.search}${window.location.hash}`
    if (next === current) return
    if (options?.replace) window.history.replaceState(null, '', next)
    else window.history.pushState(null, '', next)
    setLocation(readLocation())
    window.scrollTo?.(0, 0)
  }, [])

  const value = useMemo<RouterValue>(
    () => ({
      pathname: location.pathname,
      search: location.search,
      query: new URLSearchParams(location.search),
      navigate,
    }),
    [location, navigate],
  )

  return <RouterContext.Provider value={value}>{children}</RouterContext.Provider>
}

export function useRouter(): RouterValue {
  const value = useContext(RouterContext)
  if (!value) throw new Error('useRouter 必须在 RouterProvider 内使用')
  return value
}

export function useQueryParam(name: string): string | null {
  const { query } = useRouter()
  return query.get(name)
}

interface LinkProps extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> {
  to: string
  activeClassName?: string
  /** 精确匹配路径，默认前缀匹配。 */
  exact?: boolean
}

export function Link({
  to,
  activeClassName,
  exact = false,
  className,
  onClick,
  children,
  ...rest
}: LinkProps) {
  const { pathname, navigate } = useRouter()
  const targetPath = to.split('?')[0].split('#')[0]
  const active = exact ? pathname === targetPath : pathname === targetPath || pathname.startsWith(`${targetPath}/`)
  const classes = [className, active && activeClassName].filter(Boolean).join(' ')

  return (
    <a
      {...rest}
      href={to}
      className={classes || undefined}
      onClick={(event) => {
        onClick?.(event)
        if (event.defaultPrevented) return
        if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        event.preventDefault()
        navigate(to)
      }}
    >
      {children}
    </a>
  )
}
