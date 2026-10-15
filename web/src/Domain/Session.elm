module Domain.Session exposing
    ( ManagedUser
    , Role(..)
    , Session
    , managedUserDecoder
    , roleDecoder
    , roleName
    , sessionDecoder
    , userDecoder
    )

import Json.Decode as Decode exposing (Decoder)


type Role
    = Viewer
    | Editor
    | Administrator
    | UnknownRole String


type alias Session =
    { privateMode : Bool
    , authenticated : Bool
    , user : Maybe User
    , canRead : Bool
    , canWrite : Bool
    , canModerate : Bool
    , isAdmin : Bool
    }


type alias User =
    { id : String
    , handle : String
    , role : Role
    }


type alias ManagedUser =
    { id : String
    , handle : String
    , role : Role
    , disabled : Bool
    }


roleDecoder : Decoder Role
roleDecoder =
    Decode.string
        |> Decode.map
            (\raw ->
                case raw of
                    "viewer" ->
                        Viewer

                    "editor" ->
                        Editor

                    "admin" ->
                        Administrator

                    other ->
                        UnknownRole other
            )


roleName : Role -> String
roleName role =
    case role of
        Viewer ->
            "viewer"

        Editor ->
            "editor"

        Administrator ->
            "admin"

        UnknownRole raw ->
            raw


managedUserDecoder : Decoder ManagedUser
managedUserDecoder =
    Decode.map4 ManagedUser
        (Decode.field "id" idDecoder)
        (Decode.field "handle" Decode.string)
        (Decode.field "role" roleDecoder)
        (Decode.field "disabled" Decode.bool)


userDecoder : Decoder User
userDecoder =
    Decode.map3 User
        (Decode.field "id" idDecoder)
        (Decode.field "handle" Decode.string)
        (Decode.field "role" roleDecoder)


idDecoder : Decoder String
idDecoder =
    Decode.oneOf
        [ Decode.string
        , Decode.int |> Decode.map String.fromInt
        ]


sessionDecoder : Decoder Session
sessionDecoder =
    Decode.map7 Session
        (Decode.field "private_mode" Decode.bool)
        (Decode.field "authenticated" Decode.bool)
        (Decode.field "user" (Decode.nullable userDecoder))
        (Decode.field "can_read" Decode.bool)
        (Decode.field "can_write" Decode.bool)
        (Decode.field "can_moderate" Decode.bool)
        (Decode.field "is_admin" Decode.bool)
