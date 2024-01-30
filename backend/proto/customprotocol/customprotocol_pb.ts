/* eslint-disable */
// @generated by protobuf-ts 2.9.1 with parameter add_pb_suffix,eslint_disable,ts_nocheck,generate_dependencies,long_type_number
// @generated from protobuf file "customprotocol/customprotocol.proto" (package "customprotocol", syntax proto3)
// tslint:disable
// @ts-nocheck
import type { BinaryWriteOptions } from "@protobuf-ts/runtime";
import type { IBinaryWriter } from "@protobuf-ts/runtime";
import { WireType } from "@protobuf-ts/runtime";
import type { BinaryReadOptions } from "@protobuf-ts/runtime";
import type { IBinaryReader } from "@protobuf-ts/runtime";
import { UnknownFieldHandler } from "@protobuf-ts/runtime";
import type { PartialMessage } from "@protobuf-ts/runtime";
import { reflectionMergePartial } from "@protobuf-ts/runtime";
import { MESSAGE_TYPE } from "@protobuf-ts/runtime";
import { MessageType } from "@protobuf-ts/runtime";
// =========================== Channel ================================
//                                     / --- Outgoing messages --- \ 
//                                    { Message 1 } ... { Message M }
//                                <---
//                                --->
//  / --- Incoming messages --- \ 
// { Message 1 } ... { Message N }
// ====================================================================

/**
 * NOTE: The same structure is used for both Request and Response
 *
 * @generated from protobuf message customprotocol.Message
 */
export interface Message {
    /**
     * @generated from protobuf field: customprotocol.Meta meta = 1;
     */
    meta?: Meta;
    /**
     * @generated from protobuf field: customprotocol.Body body = 2;
     */
    body?: Body;
}
/**
 * @generated from protobuf message customprotocol.Meta
 */
export interface Meta {
    /**
     * NOTE: This field can be used for debug or ux research purposes.
     *
     * @generated from protobuf field: string trace_id = 1;
     */
    traceId: string;
    /**
     * NOTE: This identifier can be used to mark logical channel or stream. For
     * example when we establish a stream, we also need to route user requests
     * to stored (cached) streams. The same is applied when used with chunks/etc.
     *
     * @generated from protobuf field: string channel_id = 2;
     */
    channelId: string;
    /**
     * NOTE: This is a name of route this message delivered to/from.
     *
     * @generated from protobuf field: string route_name = 3;
     */
    routeName: string;
    /**
     * NOTE: Should be true if sender handler is terminating
     *
     * @generated from protobuf field: bool is_terminated = 4;
     */
    isTerminated: boolean;
    /**
     * NOTE: Indicates that outgoing message is not ready yet. Polling request
     * must be repeated after poll_delay_ms (see below).
     *
     * @generated from protobuf field: bool is_not_ready = 5;
     */
    isNotReady: boolean;
    /**
     * NOTE: Error (handler or internal) occured, its details serialized to body
     *
     * @generated from protobuf field: bool is_error = 6;
     */
    isError: boolean;
    /**
     * @generated from protobuf field: repeated customprotocol.Error errors = 7;
     */
    errors: Error[];
    /**
     * NOTE: Server will use this field to notify the client on what time it
     * should wait before doing next poll request.
     *
     * @generated from protobuf field: uint64 poll_delay_ms = 8;
     */
    pollDelayMs: number;
    /**
     * NOTE: When sent from backend, this flag means that there are no messages
     * in outgoing channel. Combined with `is_terminated` flag this one can be
     * used to prevent further polling/closing requests.
     *
     * @generated from protobuf field: bool is_empty = 9;
     */
    isEmpty: boolean;
    /**
     * NOTE: This fields are useful for organizing page navigation or data
     * transfer in similar manner
     *
     * @generated from protobuf field: string last_datum_id = 100;
     */
    lastDatumId: string;
    /**
     * @generated from protobuf field: string next_datum_id = 101;
     */
    nextDatumId: string;
}
/**
 * @generated from protobuf message customprotocol.Error
 */
export interface Error {
    /**
     * @generated from protobuf field: customprotocol.Error.Kind kind = 1;
     */
    kind: Error_Kind;
    /**
     * @generated from protobuf field: int32 code = 2;
     */
    code: number;
    /**
     * @generated from protobuf field: string message = 3;
     */
    message: string;
}
/**
 * @generated from protobuf enum customprotocol.Error.Kind
 */
export enum Error_Kind {
    /**
     * @generated from protobuf enum value: Unknown = 0;
     */
    Unknown = 0,
    /**
     * @generated from protobuf enum value: Grpc = 1;
     */
    Grpc = 1
}
/**
 * @generated from protobuf message customprotocol.Body
 */
export interface Body {
    /**
     * @generated from protobuf field: bytes content = 1;
     */
    content: Uint8Array;
}
// @generated message type with reflection information, may provide speed optimized methods
class Message$Type extends MessageType<Message> {
    constructor() {
        super("customprotocol.Message", [
            { no: 1, name: "meta", kind: "message", T: () => Meta },
            { no: 2, name: "body", kind: "message", T: () => Body }
        ]);
    }
    create(value?: PartialMessage<Message>): Message {
        const message = {};
        globalThis.Object.defineProperty(message, MESSAGE_TYPE, { enumerable: false, value: this });
        if (value !== undefined)
            reflectionMergePartial<Message>(this, message, value);
        return message;
    }
    internalBinaryRead(reader: IBinaryReader, length: number, options: BinaryReadOptions, target?: Message): Message {
        let message = target ?? this.create(), end = reader.pos + length;
        while (reader.pos < end) {
            let [fieldNo, wireType] = reader.tag();
            switch (fieldNo) {
                case /* customprotocol.Meta meta */ 1:
                    message.meta = Meta.internalBinaryRead(reader, reader.uint32(), options, message.meta);
                    break;
                case /* customprotocol.Body body */ 2:
                    message.body = Body.internalBinaryRead(reader, reader.uint32(), options, message.body);
                    break;
                default:
                    let u = options.readUnknownField;
                    if (u === "throw")
                        throw new globalThis.Error(`Unknown field ${fieldNo} (wire type ${wireType}) for ${this.typeName}`);
                    let d = reader.skip(wireType);
                    if (u !== false)
                        (u === true ? UnknownFieldHandler.onRead : u)(this.typeName, message, fieldNo, wireType, d);
            }
        }
        return message;
    }
    internalBinaryWrite(message: Message, writer: IBinaryWriter, options: BinaryWriteOptions): IBinaryWriter {
        /* customprotocol.Meta meta = 1; */
        if (message.meta)
            Meta.internalBinaryWrite(message.meta, writer.tag(1, WireType.LengthDelimited).fork(), options).join();
        /* customprotocol.Body body = 2; */
        if (message.body)
            Body.internalBinaryWrite(message.body, writer.tag(2, WireType.LengthDelimited).fork(), options).join();
        let u = options.writeUnknownFields;
        if (u !== false)
            (u == true ? UnknownFieldHandler.onWrite : u)(this.typeName, message, writer);
        return writer;
    }
}
/**
 * @generated MessageType for protobuf message customprotocol.Message
 */
export const Message = new Message$Type();
// @generated message type with reflection information, may provide speed optimized methods
class Meta$Type extends MessageType<Meta> {
    constructor() {
        super("customprotocol.Meta", [
            { no: 1, name: "trace_id", kind: "scalar", T: 9 /*ScalarType.STRING*/ },
            { no: 2, name: "channel_id", kind: "scalar", T: 9 /*ScalarType.STRING*/ },
            { no: 3, name: "route_name", kind: "scalar", T: 9 /*ScalarType.STRING*/ },
            { no: 4, name: "is_terminated", kind: "scalar", T: 8 /*ScalarType.BOOL*/ },
            { no: 5, name: "is_not_ready", kind: "scalar", T: 8 /*ScalarType.BOOL*/ },
            { no: 6, name: "is_error", kind: "scalar", T: 8 /*ScalarType.BOOL*/ },
            { no: 7, name: "errors", kind: "message", repeat: 1 /*RepeatType.PACKED*/, T: () => Error },
            { no: 8, name: "poll_delay_ms", kind: "scalar", T: 4 /*ScalarType.UINT64*/, L: 2 /*LongType.NUMBER*/ },
            { no: 9, name: "is_empty", kind: "scalar", T: 8 /*ScalarType.BOOL*/ },
            { no: 100, name: "last_datum_id", kind: "scalar", T: 9 /*ScalarType.STRING*/ },
            { no: 101, name: "next_datum_id", kind: "scalar", T: 9 /*ScalarType.STRING*/ }
        ]);
    }
    create(value?: PartialMessage<Meta>): Meta {
        const message = { traceId: "", channelId: "", routeName: "", isTerminated: false, isNotReady: false, isError: false, errors: [], pollDelayMs: 0, isEmpty: false, lastDatumId: "", nextDatumId: "" };
        globalThis.Object.defineProperty(message, MESSAGE_TYPE, { enumerable: false, value: this });
        if (value !== undefined)
            reflectionMergePartial<Meta>(this, message, value);
        return message;
    }
    internalBinaryRead(reader: IBinaryReader, length: number, options: BinaryReadOptions, target?: Meta): Meta {
        let message = target ?? this.create(), end = reader.pos + length;
        while (reader.pos < end) {
            let [fieldNo, wireType] = reader.tag();
            switch (fieldNo) {
                case /* string trace_id */ 1:
                    message.traceId = reader.string();
                    break;
                case /* string channel_id */ 2:
                    message.channelId = reader.string();
                    break;
                case /* string route_name */ 3:
                    message.routeName = reader.string();
                    break;
                case /* bool is_terminated */ 4:
                    message.isTerminated = reader.bool();
                    break;
                case /* bool is_not_ready */ 5:
                    message.isNotReady = reader.bool();
                    break;
                case /* bool is_error */ 6:
                    message.isError = reader.bool();
                    break;
                case /* repeated customprotocol.Error errors */ 7:
                    message.errors.push(Error.internalBinaryRead(reader, reader.uint32(), options));
                    break;
                case /* uint64 poll_delay_ms */ 8:
                    message.pollDelayMs = reader.uint64().toNumber();
                    break;
                case /* bool is_empty */ 9:
                    message.isEmpty = reader.bool();
                    break;
                case /* string last_datum_id */ 100:
                    message.lastDatumId = reader.string();
                    break;
                case /* string next_datum_id */ 101:
                    message.nextDatumId = reader.string();
                    break;
                default:
                    let u = options.readUnknownField;
                    if (u === "throw")
                        throw new globalThis.Error(`Unknown field ${fieldNo} (wire type ${wireType}) for ${this.typeName}`);
                    let d = reader.skip(wireType);
                    if (u !== false)
                        (u === true ? UnknownFieldHandler.onRead : u)(this.typeName, message, fieldNo, wireType, d);
            }
        }
        return message;
    }
    internalBinaryWrite(message: Meta, writer: IBinaryWriter, options: BinaryWriteOptions): IBinaryWriter {
        /* string trace_id = 1; */
        if (message.traceId !== "")
            writer.tag(1, WireType.LengthDelimited).string(message.traceId);
        /* string channel_id = 2; */
        if (message.channelId !== "")
            writer.tag(2, WireType.LengthDelimited).string(message.channelId);
        /* string route_name = 3; */
        if (message.routeName !== "")
            writer.tag(3, WireType.LengthDelimited).string(message.routeName);
        /* bool is_terminated = 4; */
        if (message.isTerminated !== false)
            writer.tag(4, WireType.Varint).bool(message.isTerminated);
        /* bool is_not_ready = 5; */
        if (message.isNotReady !== false)
            writer.tag(5, WireType.Varint).bool(message.isNotReady);
        /* bool is_error = 6; */
        if (message.isError !== false)
            writer.tag(6, WireType.Varint).bool(message.isError);
        /* repeated customprotocol.Error errors = 7; */
        for (let i = 0; i < message.errors.length; i++)
            Error.internalBinaryWrite(message.errors[i], writer.tag(7, WireType.LengthDelimited).fork(), options).join();
        /* uint64 poll_delay_ms = 8; */
        if (message.pollDelayMs !== 0)
            writer.tag(8, WireType.Varint).uint64(message.pollDelayMs);
        /* bool is_empty = 9; */
        if (message.isEmpty !== false)
            writer.tag(9, WireType.Varint).bool(message.isEmpty);
        /* string last_datum_id = 100; */
        if (message.lastDatumId !== "")
            writer.tag(100, WireType.LengthDelimited).string(message.lastDatumId);
        /* string next_datum_id = 101; */
        if (message.nextDatumId !== "")
            writer.tag(101, WireType.LengthDelimited).string(message.nextDatumId);
        let u = options.writeUnknownFields;
        if (u !== false)
            (u == true ? UnknownFieldHandler.onWrite : u)(this.typeName, message, writer);
        return writer;
    }
}
/**
 * @generated MessageType for protobuf message customprotocol.Meta
 */
export const Meta = new Meta$Type();
// @generated message type with reflection information, may provide speed optimized methods
class Error$Type extends MessageType<Error> {
    constructor() {
        super("customprotocol.Error", [
            { no: 1, name: "kind", kind: "enum", T: () => ["customprotocol.Error.Kind", Error_Kind] },
            { no: 2, name: "code", kind: "scalar", T: 5 /*ScalarType.INT32*/ },
            { no: 3, name: "message", kind: "scalar", T: 9 /*ScalarType.STRING*/ }
        ]);
    }
    create(value?: PartialMessage<Error>): Error {
        const message = { kind: 0, code: 0, message: "" };
        globalThis.Object.defineProperty(message, MESSAGE_TYPE, { enumerable: false, value: this });
        if (value !== undefined)
            reflectionMergePartial<Error>(this, message, value);
        return message;
    }
    internalBinaryRead(reader: IBinaryReader, length: number, options: BinaryReadOptions, target?: Error): Error {
        let message = target ?? this.create(), end = reader.pos + length;
        while (reader.pos < end) {
            let [fieldNo, wireType] = reader.tag();
            switch (fieldNo) {
                case /* customprotocol.Error.Kind kind */ 1:
                    message.kind = reader.int32();
                    break;
                case /* int32 code */ 2:
                    message.code = reader.int32();
                    break;
                case /* string message */ 3:
                    message.message = reader.string();
                    break;
                default:
                    let u = options.readUnknownField;
                    if (u === "throw")
                        throw new globalThis.Error(`Unknown field ${fieldNo} (wire type ${wireType}) for ${this.typeName}`);
                    let d = reader.skip(wireType);
                    if (u !== false)
                        (u === true ? UnknownFieldHandler.onRead : u)(this.typeName, message, fieldNo, wireType, d);
            }
        }
        return message;
    }
    internalBinaryWrite(message: Error, writer: IBinaryWriter, options: BinaryWriteOptions): IBinaryWriter {
        /* customprotocol.Error.Kind kind = 1; */
        if (message.kind !== 0)
            writer.tag(1, WireType.Varint).int32(message.kind);
        /* int32 code = 2; */
        if (message.code !== 0)
            writer.tag(2, WireType.Varint).int32(message.code);
        /* string message = 3; */
        if (message.message !== "")
            writer.tag(3, WireType.LengthDelimited).string(message.message);
        let u = options.writeUnknownFields;
        if (u !== false)
            (u == true ? UnknownFieldHandler.onWrite : u)(this.typeName, message, writer);
        return writer;
    }
}
/**
 * @generated MessageType for protobuf message customprotocol.Error
 */
export const Error = new Error$Type();
// @generated message type with reflection information, may provide speed optimized methods
class Body$Type extends MessageType<Body> {
    constructor() {
        super("customprotocol.Body", [
            { no: 1, name: "content", kind: "scalar", T: 12 /*ScalarType.BYTES*/ }
        ]);
    }
    create(value?: PartialMessage<Body>): Body {
        const message = { content: new Uint8Array(0) };
        globalThis.Object.defineProperty(message, MESSAGE_TYPE, { enumerable: false, value: this });
        if (value !== undefined)
            reflectionMergePartial<Body>(this, message, value);
        return message;
    }
    internalBinaryRead(reader: IBinaryReader, length: number, options: BinaryReadOptions, target?: Body): Body {
        let message = target ?? this.create(), end = reader.pos + length;
        while (reader.pos < end) {
            let [fieldNo, wireType] = reader.tag();
            switch (fieldNo) {
                case /* bytes content */ 1:
                    message.content = reader.bytes();
                    break;
                default:
                    let u = options.readUnknownField;
                    if (u === "throw")
                        throw new globalThis.Error(`Unknown field ${fieldNo} (wire type ${wireType}) for ${this.typeName}`);
                    let d = reader.skip(wireType);
                    if (u !== false)
                        (u === true ? UnknownFieldHandler.onRead : u)(this.typeName, message, fieldNo, wireType, d);
            }
        }
        return message;
    }
    internalBinaryWrite(message: Body, writer: IBinaryWriter, options: BinaryWriteOptions): IBinaryWriter {
        /* bytes content = 1; */
        if (message.content.length)
            writer.tag(1, WireType.LengthDelimited).bytes(message.content);
        let u = options.writeUnknownFields;
        if (u !== false)
            (u == true ? UnknownFieldHandler.onWrite : u)(this.typeName, message, writer);
        return writer;
    }
}
/**
 * @generated MessageType for protobuf message customprotocol.Body
 */
export const Body = new Body$Type();
