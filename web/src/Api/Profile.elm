module Api.Profile exposing
    ( Failure(..)
    , ListPage
    , ProfileList
    , clearAvatar
    , failureMessage
    , lists
    , own
    , replaceAvatar
    , save
    , view
    )

import Domain.Profile as Profile exposing (Profile)
import File exposing (File)
import Http
import Json.Decode as Decode exposing (Decoder)
import Json.Encode as Encode
import Url.Builder


type alias ProfileList =
    { id : String
    , name : String
    , visibleCount : Int
    , coverUrl : String
    }


type alias ListPage =
    { lists : List ProfileList
    , nextCursor : Maybe String
    }


type Failure
    = Rejected String
    | Conflict Profile
    | Transport Http.Error


own : String -> (Result Http.Error Profile -> msg) -> Cmd msg
own apiBase toMsg =
    Http.request
        { method = "GET"
        , headers = []
        , url = endpoint apiBase [ "api", "profile" ] []
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg Profile.decoder
        , timeout = Just 12000
        , tracker = Nothing
        }


view : String -> String -> (Result Http.Error Profile -> msg) -> Cmd msg
view apiBase username toMsg =
    Http.request
        { method = "GET"
        , headers = []
        , url = endpoint apiBase [ "api", "profiles", username ] []
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg Profile.decoder
        , timeout = Just 12000
        , tracker = Nothing
        }


lists : String -> String -> Maybe String -> (Result Http.Error ListPage -> msg) -> Cmd msg
lists apiBase username cursor toMsg =
    let
        query =
            case cursor of
                Just token ->
                    [ Url.Builder.string "cursor" token ]

                Nothing ->
                    []
    in
    Http.request
        { method = "GET"
        , headers = []
        , url = endpoint apiBase [ "api", "profiles", username, "lists" ] query
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg listPageDecoder
        , timeout = Just 12000
        , tracker = Nothing
        }


save : String -> Profile.Draft -> (Result Failure Profile -> msg) -> Cmd msg
save apiBase draft toMsg =
    Http.request
        { method = "PATCH"
        , headers = []
        , url = endpoint apiBase [ "api", "profile" ] []
        , body =
            Http.jsonBody
                (Encode.object
                    [ ( "display_name", Encode.string (String.trim draft.displayName) )
                    , ( "username", Encode.string (String.trim draft.username) )
                    , ( "bio", Encode.string (String.trim draft.bio) )
                    , ( "website", Encode.string (String.trim draft.website) )
                    , ( "expected_version", Encode.int draft.expectedVersion )
                    ]
                )
        , expect = expectProfile toMsg
        , timeout = Just 12000
        , tracker = Nothing
        }


replaceAvatar : String -> Int -> File -> (Result Failure Profile -> msg) -> Cmd msg
replaceAvatar apiBase version file toMsg =
    Http.request
        { method = "POST"
        , headers = []
        , url = endpoint apiBase [ "api", "profile", "avatar" ] []
        , body =
            Http.multipartBody
                [ Http.stringPart "expected_version" (String.fromInt version)
                , Http.filePart "avatar" file
                ]
        , expect = expectProfile toMsg
        , timeout = Just 20000
        , tracker = Nothing
        }


clearAvatar : String -> Int -> (Result Failure Profile -> msg) -> Cmd msg
clearAvatar apiBase version toMsg =
    Http.request
        { method = "DELETE"
        , headers = []
        , url = endpoint apiBase [ "api", "profile", "avatar" ] []
        , body = Http.jsonBody (Encode.object [ ( "expected_version", Encode.int version ) ])
        , expect = expectProfile toMsg
        , timeout = Just 12000
        , tracker = Nothing
        }


failureMessage : Failure -> String
failureMessage failure =
    case failure of
        Rejected message ->
            message

        Conflict _ ->
            "This profile was updated elsewhere. Your draft is still here."

        Transport httpError ->
            case httpError of
                Http.BadUrl _ ->
                    "The profile address was invalid."

                Http.Timeout ->
                    "The profile request timed out."

                Http.NetworkError ->
                    "The profile request could not reach the server."

                Http.BadStatus 404 ->
                    "Profile not found."

                Http.BadStatus 403 ->
                    "You can only edit your own profile."

                Http.BadStatus status ->
                    "Profile request failed (HTTP " ++ String.fromInt status ++ ")."

                Http.BadBody _ ->
                    "The profile response was invalid."


expectProfile : (Result Failure Profile -> msg) -> Http.Expect msg
expectProfile toMsg =
    Http.expectStringResponse toMsg <|
        \response ->
            case response of
                Http.GoodStatus_ _ body ->
                    Decode.decodeString Profile.decoder body
                        |> Result.mapError (\_ -> Transport (Http.BadBody "profile"))

                Http.BadStatus_ meta body ->
                    if meta.statusCode == 409 then
                        decodeConflict body

                    else
                        Err (Transport (Http.BadStatus meta.statusCode))

                Http.BadUrl_ url ->
                    Err (Transport (Http.BadUrl url))

                Http.Timeout_ ->
                    Err (Transport Http.Timeout)

                Http.NetworkError_ ->
                    Err (Transport Http.NetworkError)


decodeConflict : String -> Result Failure Profile
decodeConflict body =
    let
        conflictFlag =
            Decode.decodeString (Decode.field "conflict" Decode.bool) body
                |> Result.withDefault False
    in
    if conflictFlag then
        case Decode.decodeString (Decode.field "profile" Profile.decoder) body of
            Ok profile ->
                Err (Conflict profile)

            Err _ ->
                Err (Transport (Http.BadBody "profile"))

    else
        case Decode.decodeString (Decode.field "error" Decode.string) body of
            Ok message ->
                Err (Rejected message)

            Err _ ->
                Err (Rejected "profile changed")


listPageDecoder : Decoder ListPage
listPageDecoder =
    Decode.map2 ListPage
        (Decode.field "lists" (Decode.list listDecoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))


listDecoder : Decoder ProfileList
listDecoder =
    Decode.map4 ProfileList
        (Decode.field "id" Decode.string)
        (Decode.field "name" Decode.string)
        (Decode.field "visible_count" Decode.int)
        (Decode.field "cover_url" Decode.string)


endpoint : String -> List String -> List Url.Builder.QueryParameter -> String
endpoint apiBase parts query =
    if String.trim apiBase == "" then
        Url.Builder.absolute parts query

    else
        Url.Builder.crossOrigin apiBase parts query
