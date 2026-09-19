export function mediaText(zh: boolean) {
  return {
    title: zh ? '媒体中心' : 'Media Center',
    intro: zh ? '只播放本机已授权的音频和视频，没有曲库，也不会把发送当成成功。' : 'Play only local files you authorize. There is no catalog, and sending a command is not treated as success.',
    views: zh ? '媒体视图' : 'Media views',
    music: zh ? '音乐' : 'Music',
    video: zh ? '视频' : 'Video',
    pick: zh ? '选择本地媒体' : 'Choose local media',
    empty: zh ? '选择本地文件后开始播放。' : 'Choose a local file to start.',
    idle: zh ? '已就绪，等待播放' : 'Ready, waiting to play',
    idleHint: zh ? '只播放你授权的本机文件 · 无曲库' : 'Local files you authorize · no catalog',
    emptyLabel: zh ? '空状态' : 'Empty state',
    untitled: zh ? '未选择媒体' : 'No media selected',
    play: zh ? '播放' : 'Play',
    pause: zh ? '暂停' : 'Pause',
    previous: zh ? '上一首' : 'Previous',
    next: zh ? '下一首' : 'Next',
    queue: zh ? '队列' : 'Queue',
    queueLabel: zh ? '播放队列' : 'Playback queue',
    close: zh ? '关闭' : 'Close',
    remove: zh ? '移除' : 'Remove',
    clear: zh ? '清空队列' : 'Clear queue',
    emptyQueue: zh ? '队列是空的' : 'Queue is empty',
    externalQueue: zh ? '队列由外部应用管理' : 'The queue is managed by an external app',
    seek: zh ? '进度' : 'Seek',
    volume: zh ? '音量' : 'Volume',
    playing: zh ? '正在播放' : 'Playing',
    paused: zh ? '已暂停' : 'Paused',
    dispatched: zh ? '命令已发送，待核验' : 'Command sent, not verified yet',
    videoStage: zh ? '画面由本机唯一播放器输出' : 'Picture is output by the single local player',
    videoEmpty: zh ? '还没有可播放的画面' : 'No playable picture yet',
    mini: zh ? '迷你播放器' : 'Mini player',
    closing: zh ? '正在结束' : 'Closing',
    closeError: zh ? '停止未确认' : 'Stop was not confirmed',
    retryClose: zh ? '重试结束' : 'Retry close',
    operation: zh ? '媒体操作' : 'Media operation',
    pending: zh ? '处理中' : 'In progress',
    confirmed: zh ? '已确认' : 'Confirmed',
    unconfirmed: zh ? '未确认' : 'Unconfirmed',
    activity: zh ? '活动' : 'Activity',
    activityFilter: zh ? '活动筛选' : 'Activity filter',
    activityAll: zh ? '全部' : 'All',
    activityRunning: zh ? '进行中' : 'Running',
    activityFailed: zh ? '需处理' : 'Needs attention',
    activityEmpty: zh ? '没有需要显示的活动' : 'No activity to show',
    disabled: zh ? '媒体会话未启用。当前只能选择文件，还不能创建播放会话。' : 'Media sessions are off. You can choose files, but a playback session cannot be created yet.',
    pickFailed: zh ? '选择本地媒体失败' : 'Choosing local media failed',
    noFile: zh ? '没有选出可播放的音频或视频' : 'No playable audio or video was chosen',
    createFailed: zh ? '无法创建播放会话' : 'A playback session could not be created',
    commandFailed: zh ? '媒体命令失败' : 'The media command failed',
    queueFailed: zh ? '队列命令失败' : 'The queue command failed',
    refreshFailed: zh ? '媒体状态刷新失败' : 'Media status refresh failed',
    channelDown: zh ? '本机播放通道尚未接通' : 'The local playback channel is not connected yet',
    openSettings: zh ? '打开设置' : 'Open settings',
    openPlayer: zh ? '打开播放器' : 'Open player',
    retry: zh ? '重试' : 'Retry',
    handle: zh ? '处理' : 'Handle',
    historyNote: zh ? '成功完成的任务会留在历史里，不会一直占用入口。' : 'Finished tasks stay in history and do not keep occupying the entry.',
  }
}

export function playbackStatusText(zh: boolean, phase: string, verificationStatus: string): string {
  const copy = mediaText(zh)
  if (verificationStatus === 'command_dispatched') {
    return copy.dispatched
  }
  if (phase === 'playing' && verificationStatus === 'verified_playing') return copy.playing
  if (phase === 'paused' && verificationStatus === 'verified_paused') return copy.paused
  if (phase === 'idle' || phase === 'stopped') return copy.idle
  if (phase === 'playing' || phase === 'paused' || phase === 'stalled') return copy.dispatched
  return phase
}
