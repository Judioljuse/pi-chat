import { useCallback, useEffect, useRef, useState } from 'react'

// Minimal typing for the Web Speech API (not in lib.dom for all TS versions).
interface SpeechResultList {
  length: number
  [i: number]: { isFinal: boolean; 0: { transcript: string } }
}
interface SpeechRecognitionLike {
  lang: string
  interimResults: boolean
  continuous: boolean
  onresult: ((e: { resultIndex: number; results: SpeechResultList }) => void) | null
  onerror: ((e: { error: string }) => void) | null
  onend: (() => void) | null
  start(): void
  stop(): void
  abort(): void
}

const Ctor = (): (new () => SpeechRecognitionLike) | undefined => {
  if (typeof window === 'undefined') return undefined
  const w = window as unknown as { SpeechRecognition?: new () => SpeechRecognitionLike; webkitSpeechRecognition?: new () => SpeechRecognitionLike }
  return w.SpeechRecognition || w.webkitSpeechRecognition
}

export const speechSupported = () => !!Ctor() && (typeof window === 'undefined' || window.isSecureContext)

const ERRORS: Record<string, string> = {
  'not-allowed': '没有麦克风权限：请在浏览器设置里允许这个网站使用麦克风',
  'service-not-allowed': '浏览器不允许语音识别（可以改用键盘上的语音输入）',
  'audio-capture': '没有找到麦克风',
  network: '语音识别服务连不上（Chrome 的语音识别要访问 Google；可以改用键盘上的语音输入）',
  'no-speech': '没有听到声音',
  'language-not-supported': '浏览器不支持中文语音识别',
}

/**
 * Browser speech-to-text. onText(final) gets each recognized phrase; interim shows the phrase in progress.
 * Listens until stop is tapped; text still in progress at that point is kept, not dropped.
 */
export function useSpeech(onText: (text: string) => void, onError: (msg: string) => void) {
  const [listening, setListening] = useState(false)
  const [interim, setInterim] = useState('')
  const rec = useRef<SpeechRecognitionLike | null>(null)
  const cb = useRef({ onText, onError })
  cb.current = { onText, onError }

  // Text heard but not yet handed to onText: browsers (iOS Safari above all) often never mark the
  // last phrase final, so stop / end commits whatever is still pending instead of dropping it.
  const pending = useRef('')
  const flush = () => {
    const t = pending.current.trim()
    pending.current = ''
    if (t) cb.current.onText(t)
  }

  const stop = useCallback(() => rec.current?.stop(), [])

  const start = useCallback(() => {
    const C = Ctor()
    if (!C) return cb.current.onError('这个浏览器不支持语音输入，可以用键盘上的麦克风')
    if (rec.current) return
    const r = new C()
    r.lang = 'zh-CN'
    r.interimResults = true
    // Keep listening until the user taps stop: one-shot mode ends on the first pause (often before
    // anything is recognized, especially on iOS). Android Chrome repeats earlier text in every
    // continuous result, so it stays one-shot there.
    r.continuous = !/Android/i.test(navigator.userAgent)
    const done = new Set<number>()
    let heard = false
    r.onresult = (e) => {
      let live = ''
      for (let i = e.resultIndex; i < e.results.length; i++) {
        if (done.has(i)) continue
        const t = e.results[i][0].transcript
        if (t.trim()) heard = true
        if (e.results[i].isFinal) {
          done.add(i)
          if (t.trim()) cb.current.onText(t.trim())
        } else live += t
      }
      pending.current = live
      setInterim(live)
    }
    r.onerror = (e) => {
      if (e.error === 'aborted' || (e.error === 'no-speech' && heard)) return
      cb.current.onError(ERRORS[e.error] || `语音输入出错：${e.error}`)
    }
    r.onend = () => {
      flush()
      rec.current = null
      setListening(false)
      setInterim('')
    }
    pending.current = ''
    rec.current = r
    try {
      r.start()
      setListening(true)
    } catch {
      rec.current = null
      cb.current.onError('语音输入启动失败')
    }
  }, [])

  useEffect(() => () => rec.current?.abort(), [])
  return { listening, interim, start, stop }
}
