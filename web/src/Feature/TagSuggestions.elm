module Feature.TagSuggestions exposing (Model, Msg(..), Suggestion, applySuggestion, clear, context, decoder, init, lastToken, shouldFetch, suggestions, update)

import Http
import Json.Decode as Decode
import Process
import Task
import Url.Builder


type alias Suggestion =
    { tag : String, count : Int }


type alias Model =
    { items : List Suggestion
    , query : String
    , requestId : Int
    }


init : Model
init =
    { items = [], query = "", requestId = 0 }


clear : Model -> Model
clear model =
    { model | items = [], query = "", requestId = model.requestId + 1 }


suggestions : Model -> List Suggestion
suggestions m =
    m.items


type alias Context =
    { apiBase : String }


context : String -> Context
context apiBase =
    { apiBase = apiBase }


type Msg
    = QueryChanged String
    | FetchRequested Int String
    | Completed Int String (Result Http.Error (List Suggestion))


decoder : Decode.Decoder (List Suggestion)
decoder =
    Decode.field "tags" (Decode.list (Decode.map2 Suggestion (Decode.field "tag" Decode.string) (Decode.field "count" Decode.int)))


{-| Last comma-separated token, trimmed. Mirrors parseTags/splitTagDraft
syntax (comma separated) used by Inspector and upload drafts.
-}
lastToken : String -> String
lastToken draft =
    draft |> String.split "," |> List.reverse |> List.head |> Maybe.withDefault "" |> String.trim


shouldFetch : String -> Bool
shouldFetch draft =
    String.length (lastToken draft) >= 2 && String.length (lastToken draft) <= 128


{-| Replace/append the last token with the chosen tag, preserving all
other draft tags and adding a trailing separator for the next tag.
-}
applySuggestion : String -> String -> String
applySuggestion draft tag =
    let
        parts =
            String.split "," draft

        prefixParts =
            List.take (List.length parts - 1) parts

        prefix =
            if List.isEmpty prefixParts then
                ""

            else
                String.join "," prefixParts ++ ", "
    in
    prefix ++ tag ++ ", "


update : Context -> Msg -> Model -> ( Model, Cmd Msg )
update ctx msg model =
    case msg of
        QueryChanged draft ->
            let
                token =
                    lastToken draft
            in
            if not (shouldFetch draft) then
                ( { model | items = [], query = token, requestId = model.requestId + 1 }, Cmd.none )

            else if token == model.query then
                ( model, Cmd.none )

            else
                let
                    id =
                        model.requestId + 1
                in
                ( { model | items = [], query = token, requestId = id }
                , Process.sleep 200 |> Task.perform (\_ -> FetchRequested id token)
                )

        FetchRequested id query ->
            if id /= model.requestId || query /= model.query then
                ( model, Cmd.none )

            else
                ( model, Http.get { url = endpoint ctx.apiBase query, expect = Http.expectJson (Completed id query) decoder } )

        Completed id query result ->
            if id /= model.requestId || query /= model.query then
                ( model, Cmd.none )

            else
                case result of
                    Ok items ->
                        ( { model | items = items }, Cmd.none )

                    Err _ ->
                        ( { model | items = [] }, Cmd.none )


endpoint : String -> String -> String
endpoint apiBase prefix =
    let
        params =
            [ Url.Builder.string "prefix" prefix, Url.Builder.int "limit" 12 ]
    in
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "tags", "suggestions" ] params

    else
        Url.Builder.crossOrigin apiBase [ "api", "tags", "suggestions" ] params
