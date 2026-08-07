import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

/**
 * 文本筛选防抖：输入框直接进 load 依赖会导致每敲一个字符打一次接口。
 * 组件保留即时的输入态，接口只依赖这里返回的延迟值。
 */
export function useDebounced<T>(value: T, delay = 300): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay)
    return () => window.clearTimeout(timer)
  }, [value, delay])
  return debounced
}

export type RequestSeq = {
  /** 发起请求前调用，拿到本次请求的序号 */
  next: () => number
  /** setState 前调用；返回 false 说明期间已有更新的请求发出，本次响应应丢弃 */
  isLatest: (seq: number) => boolean
}

/**
 * 请求序号：并发返回时只接受最后一次发起的响应，避免旧结果覆盖新结果。
 * 卸载时序号自增，在途响应一律失效，顺带挡住卸载后 setState。
 */
export function useRequestSeq(): RequestSeq {
  const ref = useRef(0)
  const next = useCallback(() => {
    ref.current += 1
    return ref.current
  }, [])
  const isLatest = useCallback((seq: number) => ref.current === seq, [])
  useEffect(
    () => () => {
      ref.current += 1
    },
    [],
  )
  // 返回值会进 useCallback 依赖，必须保持引用稳定
  return useMemo(() => ({ next, isLatest }), [next, isLatest])
}

/**
 * 页码兜底：筛选条件收窄后 total 变小，停留在越界页会出现「第 5 / 1 页」的空列表。
 * total 变化后把 page 拉回最后一页。
 */
export function useClampPage(
  page: number,
  total: number,
  pageSize: number,
  setPage: (p: number) => void,
) {
  useEffect(() => {
    const pages = Math.max(1, Math.ceil(total / pageSize))
    if (page > pages) setPage(pages)
  }, [page, total, pageSize, setPage])
}
