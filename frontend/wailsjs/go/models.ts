export namespace model {
	
	export class Workspace {
	    id: string;
	    name: string;
	    rootPath: string;
	    providerId: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.rootPath = source["rootPath"];
	        this.providerId = source["providerId"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Provider {
	    id: string;
	    name: string;
	    command: string;
	    installed: boolean;
	    supported: boolean;
	    comingSoon: boolean;
	    path?: string;
	    version?: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new Provider(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.installed = source["installed"];
	        this.supported = source["supported"];
	        this.comingSoon = source["comingSoon"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.description = source["description"];
	    }
	}
	export class Device {
	    id: string;
	    name: string;
	    hostname: string;
	    os: string;
	    arch: string;
	    configured: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.hostname = source["hostname"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.configured = source["configured"];
	    }
	}
	export class BootstrapState {
	    version: string;
	    needsOnboarding: boolean;
	    device: Device;
	    nearbyDevices: Device[];
	    providers: Provider[];
	    selectedProviderIds: string[];
	    workspaces: Workspace[];
	
	    static createFrom(source: any = {}) {
	        return new BootstrapState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.needsOnboarding = source["needsOnboarding"];
	        this.device = this.convertValues(source["device"], Device);
	        this.nearbyDevices = this.convertValues(source["nearbyDevices"], Device);
	        this.providers = this.convertValues(source["providers"], Provider);
	        this.selectedProviderIds = source["selectedProviderIds"];
	        this.workspaces = this.convertValues(source["workspaces"], Workspace);
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
	
	export class OnboardingRequest {
	    deviceName: string;
	    selectedProviderIds: string[];
	
	    static createFrom(source: any = {}) {
	        return new OnboardingRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deviceName = source["deviceName"];
	        this.selectedProviderIds = source["selectedProviderIds"];
	    }
	}
	export class OpenWorkspaceRequest {
	    name: string;
	    providerId: string;
	    deviceId: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenWorkspaceRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.providerId = source["providerId"];
	        this.deviceId = source["deviceId"];
	    }
	}
	

}

