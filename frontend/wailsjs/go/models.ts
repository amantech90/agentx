export namespace model {
	
	export class ToolApproval {
	    kind: string;
	    tool?: string;
	    command?: string;
	    paths?: string[];
	    workingDirectory?: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolApproval(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.tool = source["tool"];
	        this.command = source["command"];
	        this.paths = source["paths"];
	        this.workingDirectory = source["workingDirectory"];
	        this.reason = source["reason"];
	    }
	}
	export class Screenshot {
	    id: string;
	    mediaType: string;
	    previewData?: string;
	
	    static createFrom(source: any = {}) {
	        return new Screenshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.mediaType = source["mediaType"];
	        this.previewData = source["previewData"];
	    }
	}
	export class ChatItem {
	    id: string;
	    turnId?: string;
	    kind: string;
	    role: string;
	    title?: string;
	    content: string;
	    screenshots?: Screenshot[];
	    approval?: ToolApproval;
	    status?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.turnId = source["turnId"];
	        this.kind = source["kind"];
	        this.role = source["role"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.screenshots = this.convertValues(source["screenshots"], Screenshot);
	        this.approval = this.convertValues(source["approval"], ToolApproval);
	        this.status = source["status"];
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
	export class SessionSnapshot {
	    workspaceId: string;
	    providerId: string;
	    providerSessionId?: string;
	    status: string;
	    queueDepth: number;
	    items: ChatItem[];
	
	    static createFrom(source: any = {}) {
	        return new SessionSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspaceId = source["workspaceId"];
	        this.providerId = source["providerId"];
	        this.providerSessionId = source["providerSessionId"];
	        this.status = source["status"];
	        this.queueDepth = source["queueDepth"];
	        this.items = this.convertValues(source["items"], ChatItem);
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
	export class RemoteDeviceState {
	    device: Device;
	    online: boolean;
	    providers: Provider[];
	    selectedProviderIds: string[];
	    workspaces: Workspace[];
	    sessions: SessionSnapshot[];
	
	    static createFrom(source: any = {}) {
	        return new RemoteDeviceState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.device = this.convertValues(source["device"], Device);
	        this.online = source["online"];
	        this.providers = this.convertValues(source["providers"], Provider);
	        this.selectedProviderIds = source["selectedProviderIds"];
	        this.workspaces = this.convertValues(source["workspaces"], Workspace);
	        this.sessions = this.convertValues(source["sessions"], SessionSnapshot);
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
	export class Workspace {
	    id: string;
	    projectId: string;
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
	        this.projectId = source["projectId"];
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
	    trusted: boolean;
	
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
	        this.trusted = source["trusted"];
	    }
	}
	export class BootstrapState {
	    version: string;
	    needsOnboarding: boolean;
	    device: Device;
	    nearbyDevices: Device[];
	    pairedDevices: Device[];
	    providers: Provider[];
	    selectedProviderIds: string[];
	    workspaces: Workspace[];
	    remoteDevices: RemoteDeviceState[];
	
	    static createFrom(source: any = {}) {
	        return new BootstrapState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.needsOnboarding = source["needsOnboarding"];
	        this.device = this.convertValues(source["device"], Device);
	        this.nearbyDevices = this.convertValues(source["nearbyDevices"], Device);
	        this.pairedDevices = this.convertValues(source["pairedDevices"], Device);
	        this.providers = this.convertValues(source["providers"], Provider);
	        this.selectedProviderIds = source["selectedProviderIds"];
	        this.workspaces = this.convertValues(source["workspaces"], Workspace);
	        this.remoteDevices = this.convertValues(source["remoteDevices"], RemoteDeviceState);
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
	export class BridgeSnapshot {
	    devices: RemoteDeviceState[];
	
	    static createFrom(source: any = {}) {
	        return new BridgeSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.devices = this.convertValues(source["devices"], RemoteDeviceState);
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
	export class PairingRequest {
	    id: string;
	    device: Device;
	    direction: string;
	    code: string;
	    status: string;
	    expiresAt: string;
	
	    static createFrom(source: any = {}) {
	        return new PairingRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.device = this.convertValues(source["device"], Device);
	        this.direction = source["direction"];
	        this.code = source["code"];
	        this.status = source["status"];
	        this.expiresAt = source["expiresAt"];
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
	export class PairingSnapshot {
	    requests: PairingRequest[];
	    pairedDevices: Device[];
	
	    static createFrom(source: any = {}) {
	        return new PairingSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requests = this.convertValues(source["requests"], PairingRequest);
	        this.pairedDevices = this.convertValues(source["pairedDevices"], Device);
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
	
	
	export class ResolveApprovalRequest {
	    deviceId?: string;
	    workspaceId: string;
	    approvalId: string;
	    decision: string;
	
	    static createFrom(source: any = {}) {
	        return new ResolveApprovalRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deviceId = source["deviceId"];
	        this.workspaceId = source["workspaceId"];
	        this.approvalId = source["approvalId"];
	        this.decision = source["decision"];
	    }
	}
	
	export class ScreenshotInput {
	    mediaType: string;
	    data: string;
	    previewData?: string;
	
	    static createFrom(source: any = {}) {
	        return new ScreenshotInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mediaType = source["mediaType"];
	        this.data = source["data"];
	        this.previewData = source["previewData"];
	    }
	}
	export class SendMessageRequest {
	    deviceId?: string;
	    workspaceId: string;
	    content: string;
	    displayContent?: string;
	    permissionMode?: string;
	    screenshot?: ScreenshotInput;
	
	    static createFrom(source: any = {}) {
	        return new SendMessageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deviceId = source["deviceId"];
	        this.workspaceId = source["workspaceId"];
	        this.content = source["content"];
	        this.displayContent = source["displayContent"];
	        this.permissionMode = source["permissionMode"];
	        this.screenshot = this.convertValues(source["screenshot"], ScreenshotInput);
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
	
	
	
	export class WorkspaceCommandRequest {
	    deviceId: string;
	    workspaceId: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceCommandRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deviceId = source["deviceId"];
	        this.workspaceId = source["workspaceId"];
	    }
	}

}

