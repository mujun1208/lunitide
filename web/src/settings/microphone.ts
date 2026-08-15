export const MICROPHONE_DEVICE_KEY='lunitide:microphone-device-id'

export function selectedMicrophoneId():string{
 try{return localStorage.getItem(MICROPHONE_DEVICE_KEY)??''}catch{return''}
}

export function saveMicrophoneId(deviceId:string):void{
 try{if(deviceId)localStorage.setItem(MICROPHONE_DEVICE_KEY,deviceId);else localStorage.removeItem(MICROPHONE_DEVICE_KEY)}catch{/* best effort */}
}

export function microphoneConstraints():MediaStreamConstraints{
 const deviceId=selectedMicrophoneId()
 return{audio:deviceId?{deviceId:{exact:deviceId}}:true}
}
