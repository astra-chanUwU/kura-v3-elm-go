module Feature.Trash exposing (Context, Model, Msg(..), OutMsg(..), context, init, isOpen, items, locked, nextCursor, pendingTrashCount, status, update, view)

import Api.Trash
import Domain.Trash exposing (TrashPage, TrashPost, TrashResult)
import Html exposing (Html, button, div, li, p, span, text, ul)
import Html.Attributes exposing (class, disabled, type_)
import Html.Events exposing (onClick)
import Http


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase, credential = credential, writesEnabled = writesEnabled, accessGeneration = accessGeneration }


type alias Model =
    { open : Bool
    , items : List TrashPost
    , nextCursor : Maybe String
    , status : Maybe String
    , pendingTrash : Maybe (List String)
    , busy : Bool
    , requestId : Int
    }


init : Model
init =
    { open = False, items = [], nextCursor = Nothing, status = Nothing, pendingTrash = Nothing, busy = False, requestId = 0 }


items : Model -> List TrashPost
items m =
    m.items


nextCursor : Model -> Maybe String
nextCursor m =
    m.nextCursor


status : Model -> Maybe String
status m =
    m.status


isOpen : Model -> Bool
isOpen m =
    m.open


pendingTrashCount : Model -> Maybe Int
pendingTrashCount m =
    Maybe.map List.length m.pendingTrash


{-| Lock clears sensitive trash rows and any pending confirmation. Late
completions are rejected by the captured generation in `update`.
-}
locked : Model -> Model
locked m =
    { m | items = [], nextCursor = Nothing, status = Nothing, pendingTrash = Nothing, busy = False, open = False }


type Msg
    = OpenRequested
    | CloseRequested
    | Loaded Int Int (Result Http.Error TrashPage)
    | LoadMoreRequested
    | RestoreRequested String
    | Restored Int (List String) (Result Http.Error (List TrashResult))
    | TrashSelectedRequested (List String)
    | TrashConfirmRequested
    | TrashCancelled
    | Trashed Int (List String) (Result Http.Error (List TrashResult))


type OutMsg
    = TrashApplied (List String)
    | RestoreApplied (List String)
    | AccessRevoked


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenRequested ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to view Trash." }, Cmd.none, [] )

            else
                let
                    id =
                        model.requestId + 1
                in
                ( { model | open = True, items = [], nextCursor = Nothing, requestId = id, status = Just "Loading Trash…" }
                , Api.Trash.list ctx.apiBase ctx.credential Nothing 60 (Loaded ctx.accessGeneration id)
                , []
                )

        CloseRequested ->
            ( { model | open = False }, Cmd.none, [] )

        Loaded generation id result ->
            if generation /= ctx.accessGeneration || id /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        let
                            merged =
                                if model.items |> List.isEmpty then
                                    page.posts

                                else
                                    model.items ++ List.filter (\p -> not (List.member p.id (List.map .id model.items))) page.posts
                        in
                        ( { model | items = merged, nextCursor = page.nextCursor, status = Nothing }, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err err ->
                        ( { model | status = Just ("Trash could not be loaded (HTTP " ++ httpStatus err ++ ").") }, Cmd.none, [] )

        LoadMoreRequested ->
            case model.nextCursor of
                Nothing ->
                    ( { model | status = Just "No more trashed posts." }, Cmd.none, [] )

                Just cursor ->
                    let
                        id =
                            model.requestId + 1
                    in
                    ( { model | requestId = id, status = Just "Loading Trash…" }
                    , Api.Trash.list ctx.apiBase ctx.credential (Just cursor) 60 (Loaded ctx.accessGeneration id)
                    , []
                    )

        RestoreRequested postId ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to restore posts." }, Cmd.none, [] )

            else if model.busy then
                ( model, Cmd.none, [] )

            else
                ( { model | busy = True, status = Just "Restoring…" }
                , Api.Trash.restore ctx.apiBase ctx.credential [ postId ] (Restored ctx.accessGeneration [ postId ])
                , []
                )

        Restored generation ids result ->
            settleRestore ctx generation ids result model

        TrashSelectedRequested ids ->
            if List.isEmpty ids then
                ( { model | status = Just "Select a post first." }, Cmd.none, [] )

            else if List.length ids > 100 then
                ( { model | status = Just "Move at most 100 posts to Trash at a time." }, Cmd.none, [] )

            else
                ( { model | pendingTrash = Just ids, status = Nothing }, Cmd.none, [] )

        TrashCancelled ->
            ( { model | pendingTrash = Nothing }, Cmd.none, [] )

        TrashConfirmRequested ->
            case model.pendingTrash of
                Nothing ->
                    ( model, Cmd.none, [] )

                Just ids ->
                    if not ctx.writesEnabled then
                        ( { model | status = Just "Unlock to move posts to Trash." }, Cmd.none, [] )

                    else if model.busy then
                        ( model, Cmd.none, [] )

                    else
                        ( { model | busy = True, status = Just "Moving to Trash…" }
                        , Api.Trash.trash ctx.apiBase ctx.credential ids (Trashed ctx.accessGeneration ids)
                        , []
                        )

        Trashed generation ids result ->
            settleTrash ctx generation ids result model


settleTrash : Context -> Int -> List String -> Result Http.Error (List TrashResult) -> Model -> ( Model, Cmd Msg, List OutMsg )
settleTrash ctx generation ids result model =
    if generation /= ctx.accessGeneration then
        case result of
            Ok rows ->
                if responseMatches ids rows then
                    ( model, Cmd.none, [ TrashApplied ids ] )

                else
                    ( model, Cmd.none, [] )

            Err _ ->
                ( model, Cmd.none, [] )

    else
        case result of
            Ok rows ->
                if responseMatches ids rows then
                    ( { model | busy = False, pendingTrash = Nothing, status = Just "Moved to Trash. Files are retained." }, Cmd.none, [ TrashApplied ids ] )

                else
                    settleTrash ctx generation ids (Err (Http.BadBody "unexpected trash result")) model

            Err (Http.BadStatus 401) ->
                ( { model | busy = False }, Cmd.none, [ AccessRevoked ] )

            Err err ->
                ( { model | busy = False, status = Just ("Move to Trash failed (HTTP " ++ httpStatus err ++ "). Selection kept.") }, Cmd.none, [] )


settleRestore : Context -> Int -> List String -> Result Http.Error (List TrashResult) -> Model -> ( Model, Cmd Msg, List OutMsg )
settleRestore ctx generation ids result model =
    if generation /= ctx.accessGeneration then
        case result of
            Ok rows ->
                if responseMatches ids rows then
                    ( model, Cmd.none, [ RestoreApplied ids ] )

                else
                    ( model, Cmd.none, [] )

            Err _ ->
                ( model, Cmd.none, [] )

    else
        case result of
            Ok rows ->
                if responseMatches ids rows then
                    ( { model | busy = False, items = List.filter (\p -> not (List.member p.id ids)) model.items, status = Just "Restored. Hidden posts stay hidden." }, Cmd.none, [ RestoreApplied ids ] )

                else
                    settleRestore ctx generation ids (Err (Http.BadBody "unexpected restore result")) model

            Err (Http.BadStatus 401) ->
                ( { model | busy = False }, Cmd.none, [ AccessRevoked ] )

            Err err ->
                ( { model | busy = False, status = Just ("Restore failed (HTTP " ++ httpStatus err ++ ").") }, Cmd.none, [] )


responseMatches : List String -> List TrashResult -> Bool
responseMatches ids rows =
    List.sort ids == List.sort (List.map .id rows)


httpStatus : Http.Error -> String
httpStatus err =
    case err of
        Http.BadStatus code ->
            String.fromInt code

        _ ->
            "network"


{-| Compact metadata-only Trash view. No image URLs are rendered.
-}
view : Model -> { onClose : msg, onRestore : String -> msg, onLoadMore : msg, onConfirmTrash : msg, onCancelTrash : msg } -> Html msg
view model msgs =
    div [ class "panel-section trash-view" ]
        ([ p [ class "panel-note" ] [ text "Trash keeps originals and metadata. Restoring a hidden post keeps it hidden." ]
         , case model.status of
            Just s ->
                p [ class "panel-note", Html.Attributes.attribute "role" "status" ] [ text s ]

            Nothing ->
                text ""
         ]
            ++ (case model.pendingTrash of
                    Just ids ->
                        [ p [ class "panel-note status-error" ] [ text ("Move " ++ String.fromInt (List.length ids) ++ " post(s) to Trash? Files are retained and posts can be restored.") ]
                        , p []
                            [ button [ class "button", type_ "button", onClick msgs.onConfirmTrash, disabled model.busy ] [ text ("Move " ++ String.fromInt (List.length ids) ++ " to Trash") ]
                            , button [ class "button button-quiet", type_ "button", onClick msgs.onCancelTrash ] [ text "Cancel" ]
                            ]
                        ]

                    Nothing ->
                        []
               )
            ++ [ ul [ class "nav-list trash-list" ]
                    (List.map
                        (\post ->
                            li [ class "nav-row trash-row" ]
                                [ span [] [ text ("#" ++ post.id ++ " · " ++ post.mediaType ++ " · " ++ String.fromInt post.width ++ "×" ++ String.fromInt post.height) ]
                                , span [ class "panel-note" ] [ text (String.join ", " (List.take 8 post.tags)) ]
                                , span [ class "panel-note" ] [ text (String.left 10 post.deletedAt ++ " " ++ post.moderationState) ]
                                , button [ class "button button-quiet", type_ "button", onClick (msgs.onRestore post.id), disabled model.busy ] [ text "Restore" ]
                                ]
                        )
                        model.items
                    )
               , if List.isEmpty model.items then
                    p [ class "panel-note" ] [ text "No trashed posts." ]

                 else
                    text ""
               , p []
                    [ button [ class "button button-quiet", type_ "button", onClick msgs.onLoadMore, disabled (model.nextCursor == Nothing) ] [ text "Load more" ]
                    , button [ class "button button-quiet", type_ "button", onClick msgs.onClose ] [ text "Close Trash" ]
                    ]
               ]
        )
