import { useEffect, useRef } from 'react'

/** Something changed from the shell (a draft dropped, a file made): lists reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('po:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('po:changed', h)
    return () => window.removeEventListener('po:changed', h)
  }, [])
}
