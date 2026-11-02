export namespace config {
	
	export class Profile {
	    id: string;
	    name: string;
	    container: string;
	    video_encoder: string;
	    audio_encoder: string;
	    video_options: Record<string, string>;
	    audio_options: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.container = source["container"];
	        this.video_encoder = source["video_encoder"];
	        this.audio_encoder = source["audio_encoder"];
	        this.video_options = source["video_options"];
	        this.audio_options = source["audio_options"];
	    }
	}
	export class Settings {
	    locale: string;
	    theme: string;
	    accent_hex: string;
	    font_family: string;
	    max_concurrent_jobs: number;
	    prevent_sleep: boolean;
	    update_source: string;
	    update_mirror_url: string;
	    auto_check_updates: boolean;
	    ffmpeg_path: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.locale = source["locale"];
	        this.theme = source["theme"];
	        this.accent_hex = source["accent_hex"];
	        this.font_family = source["font_family"];
	        this.max_concurrent_jobs = source["max_concurrent_jobs"];
	        this.prevent_sleep = source["prevent_sleep"];
	        this.update_source = source["update_source"];
	        this.update_mirror_url = source["update_mirror_url"];
	        this.auto_check_updates = source["auto_check_updates"];
	        this.ffmpeg_path = source["ffmpeg_path"];
	    }
	}

}

export namespace ffmpeg {
	
	export class CodecCapability {
	    name: string;
	    codec: string;
	    hardware: boolean;
	    backend: string;
	
	    static createFrom(source: any = {}) {
	        return new CodecCapability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.codec = source["codec"];
	        this.hardware = source["hardware"];
	        this.backend = source["backend"];
	    }
	}
	export class Capabilities {
	    containers: string[];
	    videoEncoders: CodecCapability[];
	    audioEncoders: CodecCapability[];
	    hardwareDecoders: CodecCapability[];
	    subtitleCopy: Record<string, Array<string>>;
	    subtitleBurn: string[];
	    concatSubtitles: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Capabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.containers = source["containers"];
	        this.videoEncoders = this.convertValues(source["videoEncoders"], CodecCapability);
	        this.audioEncoders = this.convertValues(source["audioEncoders"], CodecCapability);
	        this.hardwareDecoders = this.convertValues(source["hardwareDecoders"], CodecCapability);
	        this.subtitleCopy = source["subtitleCopy"];
	        this.subtitleBurn = source["subtitleBurn"];
	        this.concatSubtitles = source["concatSubtitles"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class SleepInhibitionStatus {
	    active: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SleepInhibitionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.error = source["error"];
	    }
	}

}

export namespace media {
	
	export class StreamInfo {
	    index: number;
	    type: string;
	    codec: string;
	    language: string;
	    width: number;
	    height: number;
	    frameRate: string;
	    sampleRate: string;
	    channels: number;
	
	    static createFrom(source: any = {}) {
	        return new StreamInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.type = source["type"];
	        this.codec = source["codec"];
	        this.language = source["language"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.frameRate = source["frameRate"];
	        this.sampleRate = source["sampleRate"];
	        this.channels = source["channels"];
	    }
	}
	export class MediaInfo {
	    path: string;
	    name: string;
	    container: string;
	    durationSeconds: number;
	    sizeBytes: number;
	    streams: StreamInfo[];
	
	    static createFrom(source: any = {}) {
	        return new MediaInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.container = source["container"];
	        this.durationSeconds = source["durationSeconds"];
	        this.sizeBytes = source["sizeBytes"];
	        this.streams = this.convertValues(source["streams"], StreamInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace model {
	
	export class CropRect {
	    x: number;
	    y: number;
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new CropRect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class Progress {
	    frame: number;
	    outTime: number;
	    totalDuration: number;
	    speed: number;
	
	    static createFrom(source: any = {}) {
	        return new Progress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.frame = source["frame"];
	        this.outTime = source["outTime"];
	        this.totalDuration = source["totalDuration"];
	        this.speed = source["speed"];
	    }
	}
	export class JobSpec {
	    inputs: string[];
	    outputPath: string;
	    mode: string;
	    profile: config.Profile;
	    videoStream?: number;
	    audioStreams: number[];
	    subtitleStream?: number;
	    subtitleMode: string;
	    trimStart: number;
	    trimEnd: number;
	    crop?: CropRect;
	    width: number;
	    height: number;
	    frameRate: string;
	    deinterlace: boolean;
	    hardwareDecode: string;
	    encoderOptions: Record<string, string>;
	    overwriteConfirmed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JobSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inputs = source["inputs"];
	        this.outputPath = source["outputPath"];
	        this.mode = source["mode"];
	        this.profile = this.convertValues(source["profile"], config.Profile);
	        this.videoStream = source["videoStream"];
	        this.audioStreams = source["audioStreams"];
	        this.subtitleStream = source["subtitleStream"];
	        this.subtitleMode = source["subtitleMode"];
	        this.trimStart = source["trimStart"];
	        this.trimEnd = source["trimEnd"];
	        this.crop = this.convertValues(source["crop"], CropRect);
	        this.width = source["width"];
	        this.height = source["height"];
	        this.frameRate = source["frameRate"];
	        this.deinterlace = source["deinterlace"];
	        this.hardwareDecode = source["hardwareDecode"];
	        this.encoderOptions = source["encoderOptions"];
	        this.overwriteConfirmed = source["overwriteConfirmed"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Job {
	    id: string;
	    spec: JobSpec;
	    state: string;
	    progress: Progress;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.spec = this.convertValues(source["spec"], JobSpec);
	        this.state = source["state"];
	        this.progress = this.convertValues(source["progress"], Progress);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace update {
	
	export class Asset {
	    os: string;
	    arch: string;
	    filename: string;
	    url: string;
	    sha256: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new Asset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.filename = source["filename"];
	        this.url = source["url"];
	        this.sha256 = source["sha256"];
	        this.size = source["size"];
	    }
	}
	export class Release {
	    version: string;
	    assets: Asset[];
	    signature: string;
	
	    static createFrom(source: any = {}) {
	        return new Release(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.assets = this.convertValues(source["assets"], Asset);
	        this.signature = source["signature"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

