export namespace main {
	
	export class FileView {
	    path: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new FileView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.size = source["size"];
	    }
	}
	export class Settings {
	    downloadDir: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadDir = source["downloadDir"];
	    }
	}
	export class ShareView {
	    id: string;
	    title: string;
	    files: FileView[];
	    fileCount: number;
	    total: number;
	    link: string;
	    status: string;
	    error?: string;
	    downloads: number;
	    active: number;
	    oneTime: boolean;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ShareView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.files = this.convertValues(source["files"], FileView);
	        this.fileCount = source["fileCount"];
	        this.total = source["total"];
	        this.link = source["link"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.downloads = source["downloads"];
	        this.active = source["active"];
	        this.oneTime = source["oneTime"];
	        this.createdAt = source["createdAt"];
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
	export class TransferView {
	    id: string;
	    direction: string;
	    shareId?: string;
	    title: string;
	    peer?: string;
	    status: string;
	    error?: string;
	    done: number;
	    total: number;
	    speed: number;
	    route: string;
	    routeInfo?: string;
	    files?: FileView[];
	    fileCount: number;
	    savedTo?: string;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new TransferView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.direction = source["direction"];
	        this.shareId = source["shareId"];
	        this.title = source["title"];
	        this.peer = source["peer"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.done = source["done"];
	        this.total = source["total"];
	        this.speed = source["speed"];
	        this.route = source["route"];
	        this.routeInfo = source["routeInfo"];
	        this.files = this.convertValues(source["files"], FileView);
	        this.fileCount = source["fileCount"];
	        this.savedTo = source["savedTo"];
	        this.startedAt = source["startedAt"];
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
	export class State {
	    shares: ShareView[];
	    transfers: TransferView[];
	    settings: Settings;
	    platform: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shares = this.convertValues(source["shares"], ShareView);
	        this.transfers = this.convertValues(source["transfers"], TransferView);
	        this.settings = this.convertValues(source["settings"], Settings);
	        this.platform = source["platform"];
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

