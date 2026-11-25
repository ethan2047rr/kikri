// Minimal typings for the runtime Wails serves at /wails/runtime.js
// (mapped to this file through `paths` in tsconfig.json).

export interface WailsEvent {
  name: string;
  data: unknown;
}

export declare const Call: {
  ByName(methodName: string, ...args: unknown[]): Promise<unknown>;
};

export declare const Events: {
  On(eventName: string, callback: (event: WailsEvent) => void): () => void;
};
