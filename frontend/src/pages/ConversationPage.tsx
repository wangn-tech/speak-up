import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'

import { api } from '../api/client'
import type { SessionConnection } from '../types'

type ChatMessage = { id: string; role: 'user' | 'assistant' | 'system'; text: string }
type ServerFrame = { type: string; payload: Record<string, unknown> }

export function ConversationPage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const connection = location.state as SessionConnection | null
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectRef = useRef(0)
  const heartbeatRef = useRef<number | null>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const audioContextRef = useRef<AudioContext | null>(null)
  const processorRef = useRef<AudioWorkletNode | null>(null)
  const turnRef = useRef(0)
  const replyIDRef = useRef('')
  const [status, setStatus] = useState('连接中')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [text, setText] = useState('')
  const [recording, setRecording] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!connection || !id) return
    let disposed = false
    let reconnectTimer: number | undefined

    function connect() {
      const url = new URL(connection!.ws_url)
      url.searchParams.set('session_id', id)
      url.searchParams.set('token', connection!.ws_token)
      const ws = new WebSocket(url)
      wsRef.current = ws
      ws.onopen = () => {
        reconnectRef.current = 0
        setStatus('准备就绪')
        ws.send(JSON.stringify({ type: 'start', payload: {} }))
        heartbeatRef.current = window.setInterval(() => {
          if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'heartbeat', payload: {} }))
        }, 30_000)
      }
      ws.onmessage = (event) => handleServerFrame(JSON.parse(event.data) as ServerFrame)
      ws.onerror = () => setError('连接出现异常，正在尝试恢复。')
      ws.onclose = () => {
        if (heartbeatRef.current !== null) window.clearInterval(heartbeatRef.current)
        if (disposed || reconnectRef.current >= 3) {
          setStatus('连接已断开')
          return
        }
        const delay = 1000 * 2 ** reconnectRef.current
        reconnectRef.current += 1
        setStatus(`重连中（${reconnectRef.current}/3）`)
        reconnectTimer = window.setTimeout(connect, delay)
      }
    }

    function handleServerFrame(frame: ServerFrame) {
      const turnSeq = Number(frame.payload.turn_seq ?? turnRef.current)
      if (frame.type === 'asr_final') {
        const value = String(frame.payload.text ?? '')
        setMessages((current) => [...current, { id: `user-${turnSeq}`, role: 'user', text: value }])
        setStatus('AI 思考中')
      } else if (frame.type === 'reply_delta') {
        const delta = String(frame.payload.delta ?? '')
        const replyID = replyIDRef.current || `assistant-${turnSeq}`
        replyIDRef.current = replyID
        setMessages((current) => {
          const existing = current.findIndex((message) => message.id === replyID)
          if (existing < 0) return [...current, { id: replyID, role: 'assistant', text: delta }]
          return current.map((message, index) => index === existing ? { ...message, text: message.text + delta } : message)
        })
        setStatus('AI 回复中')
      } else if (frame.type === 'tts_chunk' && typeof frame.payload.audio_base64 === 'string') {
        const format = String(frame.payload.format ?? 'mp3')
        void new Audio(`data:audio/${format};base64,${frame.payload.audio_base64}`).play()
      } else if (frame.type === 'turn_end') {
        replyIDRef.current = ''
        setStatus('轮到你了')
      } else if (frame.type === 'error') {
        setError(String(frame.payload.message ?? '对话发生错误'))
        setStatus(frame.payload.retriable ? '可以重试' : '暂不可用')
      }
    }

    connect()
    return () => {
      disposed = true
      if (reconnectTimer) window.clearTimeout(reconnectTimer)
      if (heartbeatRef.current !== null) window.clearInterval(heartbeatRef.current)
      wsRef.current?.close()
      streamRef.current?.getTracks().forEach((track) => track.stop())
      void audioContextRef.current?.close()
    }
  }, [connection, id])

  function sendText(event: FormEvent) {
    event.preventDefault()
    const value = text.trim()
    if (!value || wsRef.current?.readyState !== WebSocket.OPEN) return
    turnRef.current += 1
    setMessages((current) => [...current, { id: `user-${turnRef.current}`, role: 'user', text: value }])
    wsRef.current.send(JSON.stringify({ type: 'text', payload: { turn_seq: turnRef.current, text: value } }))
    setText('')
    setStatus('AI 思考中')
  }

  async function startRecording() {
    setError('')
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } })
      const context = new AudioContext()
      await context.audioWorklet.addModule('/pcm-processor.js')
      const source = context.createMediaStreamSource(stream)
      const processor = new AudioWorkletNode(context, 'pcm-processor')
      const silent = context.createGain()
      silent.gain.value = 0
      processor.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
        if (wsRef.current?.readyState === WebSocket.OPEN) wsRef.current.send(event.data)
      }
      source.connect(processor).connect(silent).connect(context.destination)
      streamRef.current = stream
      audioContextRef.current = context
      processorRef.current = processor
      turnRef.current += 1
      wsRef.current?.send(JSON.stringify({ type: 'audio_start', payload: { turn_seq: turnRef.current } }))
      setRecording(true)
      setStatus('正在录音')
    } catch {
      setError('无法使用麦克风，请授权后重试，或继续使用文字输入。')
    }
  }

  async function stopRecording() {
    processorRef.current?.disconnect()
    streamRef.current?.getTracks().forEach((track) => track.stop())
    await audioContextRef.current?.close()
    wsRef.current?.send(JSON.stringify({ type: 'audio_end', payload: { turn_seq: turnRef.current } }))
    setRecording(false)
    setStatus('正在识别')
  }

  async function finish() {
    if (recording) await stopRecording()
    wsRef.current?.send(JSON.stringify({ type: 'end', payload: {} }))
    await api.endSession(id)
    navigate(`/feedback/${id}`)
  }

  if (!connection) {
    return <section className="page narrow"><div className="panel"><h1>连接已过期</h1><p>请返回场景页重新创建练习。</p><button className="primary" onClick={() => navigate('/scenes')}>返回场景</button></div></section>
  }

  return (
    <section className="conversation-page">
      <header className="conversation-header"><div><p className="eyebrow">LIVE PRACTICE</p><h1>场景对话</h1></div><div className="status-dot"><i />{status}</div><button className="secondary" onClick={() => void finish()}>结束练习</button></header>
      <div className="conversation-body">
        <div className="messages" aria-live="polite">
          {messages.length === 0 ? <div className="empty-chat"><span>Say hello</span><p>点击录音按钮，或者输入第一句话。</p></div> : null}
          {messages.map((message) => <div className={`message ${message.role}`} key={message.id}><small>{message.role === 'user' ? 'YOU' : 'SPEAKUP'}</small><p>{message.text}</p></div>)}
        </div>
        <div className="composer panel">
          {error ? <p className="error-text">{error}</p> : null}
          <div className="voice-row">
            <button className={`record ${recording ? 'active' : ''}`} onClick={() => void (recording ? stopRecording() : startRecording())}>●</button>
            <span>{recording ? '点击结束录音' : '点击开始录音'}</span>
          </div>
          <form onSubmit={sendText}><input value={text} onChange={(event) => setText(event.target.value)} placeholder="也可以输入英文…" /><button className="primary">发送</button></form>
        </div>
      </div>
    </section>
  )
}
