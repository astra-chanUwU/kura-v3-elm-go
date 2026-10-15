module Api.Session exposing
    ( CreateUser
    , UpdateUser
    , createUser
    , endpoint
    , login
    , logout
    , read
    , updateUser
    , users
    )

import Domain.Session as Session exposing (ManagedUser, Role, Session)
import Http
import Json.Decode as Decode
import Json.Encode as Encode
import String
import Url.Builder


type alias CreateUser =
    { handle : String
    , password : String
    , role : Role
    }


type alias UpdateUser =
    { role : Maybe Role
    , disabled : Maybe Bool
    }


endpoint : String -> List String -> String
endpoint apiBase parts =
    if String.trim apiBase == "" then
        Url.Builder.absolute parts []

    else
        Url.Builder.crossOrigin apiBase parts []


read : String -> (Result Http.Error Session -> msg) -> Cmd msg
read apiBase toMsg =
    request "GET" (endpoint apiBase [ "api", "auth", "session" ]) Http.emptyBody Session.sessionDecoder toMsg


login : String -> String -> String -> (Result Http.Error Session -> msg) -> Cmd msg
login apiBase handle password toMsg =
    request "POST"
        (endpoint apiBase [ "api", "auth", "login" ])
        (Http.jsonBody
            (Encode.object
                [ ( "handle", Encode.string handle )
                , ( "password", Encode.string password )
                ]
            )
        )
        Session.sessionDecoder
        toMsg


logout : String -> (Result Http.Error Session -> msg) -> Cmd msg
logout apiBase toMsg =
    request "POST" (endpoint apiBase [ "api", "auth", "logout" ]) Http.emptyBody Session.sessionDecoder toMsg


users : String -> (Result Http.Error (List ManagedUser) -> msg) -> Cmd msg
users apiBase toMsg =
    request "GET"
        (endpoint apiBase [ "api", "auth", "users" ])
        Http.emptyBody
        (Decode.field "users" (Decode.list Session.managedUserDecoder))
        toMsg


createUser : String -> CreateUser -> (Result Http.Error ManagedUser -> msg) -> Cmd msg
createUser apiBase draft toMsg =
    request "POST"
        (endpoint apiBase [ "api", "auth", "users" ])
        (Http.jsonBody
            (Encode.object
                [ ( "handle", Encode.string draft.handle )
                , ( "password", Encode.string draft.password )
                , ( "role", Encode.string (Session.roleName draft.role) )
                ]
            )
        )
        (Decode.field "user" Session.managedUserDecoder)
        toMsg


updateUser : String -> String -> UpdateUser -> (Result Http.Error ManagedUser -> msg) -> Cmd msg
updateUser apiBase userId update toMsg =
    let
        fields =
            (case update.role of
                Nothing ->
                    []

                Just role ->
                    [ ( "role", Encode.string (Session.roleName role) ) ]
            )
                ++ (case update.disabled of
                        Nothing ->
                            []

                        Just disabled ->
                            [ ( "disabled", Encode.bool disabled ) ]
                   )
    in
    request "PATCH"
        (endpoint apiBase [ "api", "auth", "users", userId ])
        (Http.jsonBody (Encode.object fields))
        (Decode.field "user" Session.managedUserDecoder)
        toMsg


request : String -> String -> Http.Body -> Decode.Decoder a -> (Result Http.Error a -> msg) -> Cmd msg
request method url body decoder toMsg =
    Http.request
        { method = method
        , headers = []
        , url = url
        , body = body
        , expect = Http.expectJson toMsg decoder
        , timeout = Just 12000
        , tracker = Nothing
        }
