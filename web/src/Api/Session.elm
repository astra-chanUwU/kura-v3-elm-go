module Api.Session exposing
    ( CreateUser
    , InvitationPreview
    , InvitationDraft
    , UpdateUser
    , changePassword
    , createUser
    , createInvitation
    , deleteInvitation
    , endpoint
    , invitations
    , previewInvitation
    , register
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


type alias InvitationDraft =
    { role : Role }


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


changePassword : String -> String -> String -> (Result Http.Error Session -> msg) -> Cmd msg
changePassword apiBase currentPassword newPassword toMsg =
    request "POST"
        (endpoint apiBase [ "api", "auth", "password" ])
        (Http.jsonBody
            (Encode.object
                [ ( "current_password", Encode.string currentPassword )
                , ( "new_password", Encode.string newPassword )
                ]
            )
        )
        Session.sessionDecoder
        toMsg


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


previewInvitation : String -> String -> (Result Http.Error InvitationPreview -> msg) -> Cmd msg
previewInvitation apiBase token toMsg =
    request "POST"
        (endpoint apiBase [ "api", "auth", "invitations", "preview" ])
        (Http.jsonBody (Encode.object [ ( "token", Encode.string token ) ]))
        invitationPreviewDecoder
        toMsg


register : String -> String -> String -> String -> (Result Http.Error Session.Session -> msg) -> Cmd msg
register apiBase token handle password toMsg =
    request "POST"
        (endpoint apiBase [ "api", "auth", "register" ])
        (Http.jsonBody
            (Encode.object
                [ ( "token", Encode.string token )
                , ( "handle", Encode.string handle )
                , ( "password", Encode.string password )
                ]
            )
        )
        Session.sessionDecoder
        toMsg


invitations : String -> (Result Http.Error (List Session.Invitation) -> msg) -> Cmd msg
invitations apiBase toMsg =
    request "GET"
        (endpoint apiBase [ "api", "auth", "invitations" ])
        Http.emptyBody
        (Decode.field "invitations" (Decode.list Session.invitationDecoder))
        toMsg


createInvitation : String -> InvitationDraft -> (Result Http.Error ( Session.Invitation, String ) -> msg) -> Cmd msg
createInvitation apiBase draft toMsg =
    request "POST"
        (endpoint apiBase [ "api", "auth", "invitations" ])
        (Http.jsonBody (Encode.object [ ( "role", Encode.string (Session.roleName draft.role) ) ]))
        (Decode.map2 Tuple.pair
            (Decode.field "invitation" Session.invitationDecoder)
            (Decode.field "token" Decode.string)
        )
        toMsg


deleteInvitation : String -> String -> (Result Http.Error () -> msg) -> Cmd msg
deleteInvitation apiBase invitationId toMsg =
    Http.request
        { method = "DELETE"
        , headers = []
        , url = endpoint apiBase [ "api", "auth", "invitations", invitationId ]
        , body = Http.emptyBody
        , expect = Http.expectWhatever toMsg
        , timeout = Just 12000
        , tracker = Nothing
        }


type alias InvitationPreview =
    { role : Role
    , expiresAt : String
    , libraryName : String
    }


invitationPreviewDecoder : Decode.Decoder InvitationPreview
invitationPreviewDecoder =
    Decode.map3 InvitationPreview
        (Decode.field "role" Session.roleDecoder)
        (Decode.field "expires_at" Decode.string)
        (Decode.field "library_name" Decode.string)


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
